package cloudflare

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"mailer/internal/config"
)

func TestSignIn(t *testing.T) {
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	apiBase, oauthBase = srv.URL, srv.URL+"/oauth2"

	cfgm, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Setup{Cfg: cfgm, Log: log.New(io.Discard, "", 0)}
	ctx := context.Background()

	// The "browser" approves and Cloudflare redirects back with the code.
	approve := func(u string) {
		auth, _ := url.Parse(u)
		q := auth.Query()
		if q.Get("client_id") != oauthClientID || q.Get("redirect_uri") != oauthRedirect ||
			q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
			t.Errorf("auth URL: %s", u)
		}
		go func() {
			resp, err := http.Get(oauthRedirect + "?" + url.Values{"code": {"code-1"}, "state": {q.Get("state")}}.Encode())
			if err == nil {
				resp.Body.Close()
			}
		}()
	}
	if err := s.SignIn(ctx, approve); err != nil {
		t.Fatal(err)
	}
	cf := cfgm.Get().Cloudflare
	if cf.Token != "access-1" || cf.RefreshToken != "refresh-1" || time.Until(cf.TokenExpiry) < 50*time.Minute {
		t.Fatalf("sign-in not saved: %+v", cf)
	}
	if zones, err := s.Zones(ctx); err != nil || len(zones) != 2 {
		t.Fatalf("zones with the sign-in: %v %v", zones, err)
	}

	// An expiring access token is renewed before use.
	cfgm.Update(func(c *config.Config) { c.Cloudflare.TokenExpiry = time.Now() })
	if _, err := s.Zones(ctx); err != nil {
		t.Fatal(err)
	}
	if cf := cfgm.Get().Cloudflare; cf.Token != "access-2" || cf.RefreshToken != "refresh-2" {
		t.Errorf("not renewed: %+v", cf)
	}

	// A state that doesn't match is refused.
	forged := func(string) {
		go http.Get(oauthRedirect + "?code=code-1&state=forged")
	}
	if err := s.SignIn(ctx, forged); err == nil {
		t.Error("forged state accepted")
	}

	// An API token replaces the sign-in.
	s.UseToken("good-token")
	if cf := cfgm.Get().Cloudflare; cf.Token != "good-token" || cf.RefreshToken != "" {
		t.Errorf("token not saved: %+v", cf)
	}
}
