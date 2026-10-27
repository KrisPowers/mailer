package cloudflare

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"mailer/internal/config"
)

// Signing in uses the OAuth app of Cloudflare's own "cf" command-line tool,
// the first-party app whose registration covers tunnels, DNS records,
// Workers, Email Routing and Email Sending. Cloudflare only redirects back
// to the address registered for it.
const (
	oauthClientID = "cbca97e7-c331-4cdd-8fd8-e25a451b98bf"
	oauthRedirect = "http://localhost:8877/oauth/callback"
	oauthPort     = "8877"
)

var oauthBase = "https://dash.cloudflare.com/oauth2" // var for tests

// oauthScopes is what Mailer does with the account: read accounts and
// zones, run the tunnel, publish DNS records, deploy the Email Worker, and
// configure Email Routing and Email Sending. offline_access keeps it signed
// in.
var oauthScopes = []string{
	"account:read", "user:read", "zone:read", "argotunnel.write", "dns_records:edit",
	"workers_scripts:write", "email_routing:write", "email_sending:write", "offline_access",
}

type oauthToken struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func randomString(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// SignIn runs Cloudflare's browser sign-in and saves the credential: it
// hands Cloudflare's consent page to open, for the dashboard to show, and
// waits up to five minutes for Cloudflare to send the browser back here.
func (s *Setup) SignIn(ctx context.Context, open func(url string)) error {
	verifier := randomString(48)
	sum := sha256.Sum256([]byte(verifier))
	state := randomString(24)

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /oauth/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		var res result
		switch {
		case q.Get("error") != "":
			res.err = fmt.Errorf("Cloudflare sign-in: %s", firstOf(q.Get("error_description"), q.Get("error")))
		case q.Get("state") != state || q.Get("code") == "":
			res.err = errors.New("Cloudflare sign-in didn't come back as expected; try again")
		default:
			res.code = q.Get("code")
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		msg := "Mailer is connected to Cloudflare. You can close this tab."
		if res.err != nil {
			msg = res.err.Error()
		}
		fmt.Fprintf(w, `<!doctype html><title>Mailer</title><body style="font:16px system-ui,sans-serif;max-width:32em;margin:4em auto">`+
			`<p>%s</p></body>`, html.EscapeString(msg))
		select {
		case done <- res:
		default:
		}
	})

	// The browser may resolve localhost to either address.
	addrs := []string{"127.0.0.1:" + oauthPort, "[::1]:" + oauthPort}
	if s.CallbackHost != "" {
		addrs = []string{net.JoinHostPort(s.CallbackHost, oauthPort)}
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	listening := false
	for _, addr := range addrs {
		if l, err := net.Listen("tcp", addr); err == nil {
			listening = true
			go srv.Serve(l)
		}
	}
	if !listening {
		return errors.New("port " + oauthPort + " is in use, and Cloudflare sign-in comes back on it; close whatever uses it and try again")
	}
	defer srv.Close()

	open(oauthBase + "/auth?" + url.Values{
		"response_type":         {"code"},
		"client_id":             {oauthClientID},
		"redirect_uri":          {oauthRedirect},
		"scope":                 {strings.Join(oauthScopes, " ")},
		"state":                 {state},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}.Encode())

	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var res result
	select {
	case res = <-done:
	case <-ctx.Done():
		return errors.New("Cloudflare sign-in wasn't finished in the browser")
	}
	if res.err != nil {
		return res.err
	}
	t, err := exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {res.code},
		"redirect_uri": {oauthRedirect}, "code_verifier": {verifier}})
	if err != nil {
		return err
	}
	s.Log.Printf("cloudflare: signed in")
	return s.saveToken(t)
}

// UseToken saves an API token in place of a sign-in.
func (s *Setup) UseToken(token string) error {
	return s.Cfg.Update(func(c *config.Config) {
		c.Cloudflare.Token = strings.TrimSpace(token)
		c.Cloudflare.RefreshToken = ""
		c.Cloudflare.TokenExpiry = time.Time{}
	})
}

// client returns an API client with a working credential: the API token as
// is, or the sign-in's access token, renewed when it expires within a
// minute.
func (s *Setup) client(ctx context.Context) (*Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cf := s.Cfg.Get().Cloudflare
	if cf.Token == "" {
		return nil, errors.New("Cloudflare is not connected")
	}
	if cf.RefreshToken == "" || time.Until(cf.TokenExpiry) > time.Minute {
		return New(cf.Token), nil
	}
	t, err := exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {cf.RefreshToken}})
	if err != nil {
		return nil, fmt.Errorf("renewing the Cloudflare sign-in (sign in again on the Settings page): %w", err)
	}
	if err := s.saveToken(t); err != nil {
		return nil, err
	}
	return New(t.AccessToken), nil
}

func (s *Setup) saveToken(t *oauthToken) error {
	if t.ExpiresIn <= 0 {
		t.ExpiresIn = 3600
	}
	return s.Cfg.Update(func(c *config.Config) {
		c.Cloudflare.Token = t.AccessToken
		if t.RefreshToken != "" {
			c.Cloudflare.RefreshToken = t.RefreshToken
		}
		c.Cloudflare.TokenExpiry = time.Now().Add(time.Duration(t.ExpiresIn) * time.Second)
	})
}

// exchange posts a grant to Cloudflare's token endpoint.
func exchange(ctx context.Context, form url.Values) (*oauthToken, error) {
	form.Set("client_id", oauthClientID)
	req, err := http.NewRequestWithContext(ctx, "POST", oauthBase+"/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var t oauthToken
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&t); err != nil || t.AccessToken == "" {
		return nil, fmt.Errorf("Cloudflare sign-in: %s", firstOf(t.ErrorDescription, t.Error, resp.Status))
	}
	return &t, nil
}

func firstOf(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
