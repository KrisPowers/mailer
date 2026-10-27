// Package admin serves the local web UI used to set up and manage the
// server. It listens on loopback only.
package admin

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"mailer/internal/cloudflare"
	"mailer/internal/config"
	"mailer/internal/logbuf"
	"mailer/internal/mta"
	"mailer/internal/queue"
	"mailer/internal/store"
	"mailer/internal/tailscale"
	"mailer/internal/tunnel"
	"mailer/internal/users"
)

//go:embed web
var webFS embed.FS

type Server struct {
	Cfg    *config.Manager
	Users  *users.DB
	Store  *store.Store
	Queue  *queue.Queue
	MTA    *mta.MTA
	Setup  *cloudflare.Setup
	Tunnel *tunnel.Runner
	Logs   *logbuf.Buffer
	Log    *log.Logger
	// OpenURL opens a link in the default browser; the app window can't.
	OpenURL func(url string)

	mu     sync.Mutex
	checks *Checks
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	web, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServerFS(web))

	mux.HandleFunc("GET /api/overview", s.overview)
	mux.HandleFunc("POST /api/check", s.runChecksNow)
	mux.HandleFunc("POST /api/cloudflare/signin", s.cloudflareSignIn)
	mux.HandleFunc("POST /api/cloudflare/connect", s.cloudflareConnect)
	mux.HandleFunc("GET /api/cloudflare/zones", s.cloudflareZones)
	mux.HandleFunc("GET /api/domains", s.listDomains)
	mux.HandleFunc("POST /api/domains/remove", s.removeDomain)
	mux.HandleFunc("POST /api/tailscale/enable", s.tailscaleEnable)
	mux.HandleFunc("GET /api/accounts", s.listAccounts)
	mux.HandleFunc("POST /api/accounts", s.createAccount)
	mux.HandleFunc("POST /api/accounts/password", s.resetPassword)
	mux.HandleFunc("POST /api/accounts/delete", s.deleteAccount)
	mux.HandleFunc("GET /api/aliases", s.getAliases)
	mux.HandleFunc("PUT /api/aliases", s.putAliases)
	mux.HandleFunc("GET /api/queue", s.getQueue)
	mux.HandleFunc("POST /api/queue/retry", s.retryQueue)
	mux.HandleFunc("GET /api/logs", s.getLogs)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	mux.HandleFunc("POST /api/open", s.openURL)
	return s.guard(mux)
}

// guard rejects requests that didn't come from a page on this machine:
// the Host header must be a loopback name or the app window's in-process
// origin (defeats DNS rebinding), and
// state-changing requests must carry a custom header, which other sites
// can't send cross-origin.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil {
			host = r.Host
		}
		switch host {
		case "localhost", "127.0.0.1", "::1", "[::1]", "wails.localhost", "wails":
		default:
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-Mailer") != "1" {
			http.Error(w, "missing X-Mailer header", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

// ---- checks ----

// Check is one line of the status: what was checked, the result, and for
// a failure, what to do about it. Area groups it under a status icon.
type Check struct {
	Area   string `json:"area"` // cloudflare, tailscale, sending
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
	Fix    string `json:"fix,omitempty"`
}

type Checks struct {
	At         time.Time         `json:"at"`
	Cloudflare cloudflare.Status `json:"cloudflare"`
	Tailscale  tailscale.Status  `json:"tailscale"`
	Tunnel     tunnel.Status     `json:"tunnel"`
	Items      []Check           `json:"items"`
}

// RunChecks refreshes the connection checks every minute.
func (s *Server) RunChecks(ctx context.Context) {
	for {
		s.refresh(ctx)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Minute):
		}
	}
}

func (s *Server) refresh(ctx context.Context) *Checks {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cfg := s.Cfg.Get()
	c := &Checks{At: time.Now()}

	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		c.Cloudflare = s.Setup.CheckStatus(ctx, probeHealth)
	}()
	go func() {
		defer wg.Done()
		c.Tailscale = tailscale.GetStatus(ctx, cfg.Ports.IMAP, cfg.Ports.Submission)
	}()
	var port25 error
	go func() {
		defer wg.Done()
		port25 = probeSMTP(ctx, "gmail-smtp-in.l.google.com:25")
	}()
	wg.Wait()
	c.Tunnel = s.Tunnel.Status()

	cf := c.Cloudflare
	add := func(area, name string, ok bool, detail, fix string) {
		if ok {
			fix = ""
		}
		c.Items = append(c.Items, Check{area, name, ok, detail, fix})
	}
	const repair = "Click Repair: it re-applies the Cloudflare setup."
	if !cf.Connected {
		add("cloudflare", "Cloudflare", false, "Not connected yet", "Click Connect Cloudflare in setup.")
	} else {
		tunnelDetail := "not reporting"
		if cf.TunnelStatus != "" {
			tunnelDetail = fmt.Sprintf("%s · %d connection(s)", cf.TunnelStatus, cf.TunnelConns)
		}
		if cf.Error != "" {
			tunnelDetail = cf.Error
		}
		add("cloudflare", "Cloudflare tunnel", cf.TunnelStatus == "healthy", tunnelDetail,
			"The connection from this computer to Cloudflare is down, so new mail waits at Cloudflare. "+
				"Mailer restarts it by itself. If it stays down: "+repair)
		for _, d := range cf.Domains {
			add("cloudflare", "Domain "+d.Name, d.OK(), domainDetail(d), repair)
		}
		inboundDetail := "reachable from the internet"
		if !cf.PublicHealthy {
			inboundDetail = cfg.Cloudflare.InboundHost + " not reachable"
		}
		add("cloudflare", "Incoming mail path", cf.PublicHealthy, inboundDetail,
			"Cloudflare can't reach this computer, so new mail waits at Cloudflare. "+repair)
	}
	ts := c.Tailscale
	const enable = "Click Enable Funnel."
	switch {
	case !ts.Installed:
		add("tailscale", "Tailscale", false, "Not installed", "Install Tailscale from tailscale.com/download and sign in.")
	case !ts.Running:
		add("tailscale", "Tailscale", false, "Not signed in ("+orDash(ts.State)+")", "Open Tailscale on this computer and sign in.")
	default:
		add("tailscale", "Tailscale", true, ts.DNSName, "")
		add("tailscale", "Funnel for reading mail (IMAP :443)", ts.FunnelIMAP, ts.DNSName+":443",
			"Outlook and the Gmail app can't reach your inbox. "+enable)
		add("tailscale", "Funnel for sending mail (SMTP :8443)", ts.FunnelSubmission, ts.DNSName+":8443",
			"Outlook and the Gmail app can't send. "+enable)
		if ts.FunnelIMAP {
			err := probeIMAP(ctx, ts.DNSName+":443")
			add("tailscale", "Mail server answers over Funnel", err == nil, errText(err, "greeting received"),
				"Funnel is on but the mail server didn't answer through it. "+enable)
		}
	}
	if cfg.SendViaCloudflare {
		add("sending", "Sending", true, "through Cloudflare Email Sending", "")
	} else {
		add("sending", "Outbound port 25", port25 == nil, errText(port25, "your internet provider allows sending mail"),
			"Your internet provider blocks port 25, so mail to other servers can't leave this computer and waits in the queue. "+
				"Turn on Send through Cloudflare in Settings (needs Cloudflare's Workers Paid plan).")
	}
	if t := s.MTA.LastInbound(); !t.IsZero() {
		add("sending", "Last message received", true, t.Format("Jan 2 15:04"), "")
	}

	s.mu.Lock()
	s.checks = c
	s.mu.Unlock()
	return c
}

// domainDetail names the first failing check, if any.
func domainDetail(d cloudflare.DomainStatus) string {
	if d.Error != "" {
		return d.Error
	}
	for _, c := range d.Checks {
		if !c.OK {
			return c.Name + ": " + c.Detail
		}
	}
	return "receiving · MX, SPF, DKIM and DMARC in place"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func errText(err error, okText string) string {
	if err != nil {
		return err.Error()
	}
	return okText
}

func probeHealth(ctx context.Context, url string) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 100))
	if resp.StatusCode != 200 || !strings.Contains(string(b), "mailer ok") {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

func probeSMTP(ctx context.Context, addr string) error {
	d := net.Dialer{Timeout: 8 * time.Second}
	c, err := d.DialContext(ctx, "tcp4", addr)
	if err != nil {
		return errors.New("blocked or unreachable (your ISP may block port 25)")
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(8 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "220") {
		return errors.New("connected but no SMTP greeting")
	}
	return nil
}

func probeIMAP(ctx context.Context, addr string) error {
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "* OK") {
		return errors.New("no IMAP greeting")
	}
	return nil
}

func (s *Server) currentChecks() *Checks {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checks
}

// ---- handlers ----

type clientSettings struct {
	Server         string `json:"server"`
	IMAPPort       int    `json:"imap_port"`
	SubmissionPort int    `json:"smtp_port"`
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	checks := s.currentChecks()
	q, _ := s.Queue.List()
	dnsName := cfg.Tailscale.DNSName
	if checks != nil && checks.Tailscale.DNSName != "" {
		dnsName = checks.Tailscale.DNSName
	}
	var client *clientSettings
	if dnsName != "" {
		client = &clientSettings{dnsName, config.PublicIMAPPort, config.PublicSubmissionPort}
	}
	writeJSON(w, map[string]any{
		"domain":              cfg.Domain(),
		"domains":             cfg.Domains,
		"hostname":            cfg.Hostname,
		"cloudflare_setup":    cfg.Connected(),
		"tailscale_setup":     !cfg.Tailscale.EnabledAt.IsZero(),
		"send_via_cloudflare": cfg.SendViaCloudflare,
		"accounts":            len(s.Users.List()),
		"queue":               len(q),
		"checks":              checks,
		"client":              client,
		"last_inbound":        s.MTA.LastInbound(),
		"connected_at":        cfg.Cloudflare.ConnectedAt,
		"funnel_at":           cfg.Tailscale.EnabledAt,
	})
}

func (s *Server) runChecksNow(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.refresh(r.Context()))
}

// cloudflareSignIn is the one-click connect: Cloudflare's sign-in page
// opens in the browser, and once it's approved the setup runs.
func (s *Server) cloudflareSignIn(w http.ResponseWriter, r *http.Request) {
	if s.OpenURL == nil {
		writeErr(w, 500, errors.New("no browser to sign in with"))
		return
	}
	if err := s.Setup.SignIn(r.Context(), s.OpenURL); err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	s.connect(w, r, "")
}

// cloudflareConnect sets up zone (or repairs it) with the saved sign-in,
// or with an API token when one is given.
func (s *Server) cloudflareConnect(w http.ResponseWriter, r *http.Request) {
	var req struct{ Token, Zone string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Token != "" {
		if err := s.Setup.UseToken(req.Token); err != nil {
			writeErr(w, 500, err)
			return
		}
	}
	s.connect(w, r, req.Zone)
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request, zone string) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	steps, err := s.Setup.Connect(ctx, zone)
	var zc *cloudflare.ZoneChoiceError
	if errors.As(err, &zc) {
		writeJSON(w, map[string]any{"choose_zone": zc.Zones})
		return
	}
	resp := map[string]any{"steps": steps}
	if err != nil {
		resp["error"] = err.Error()
	}
	go s.refresh(context.Background())
	writeJSON(w, resp)
}

type zoneInfo struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
}

// cloudflareZones lists the domains the Cloudflare account has, for adding one.
func (s *Server) cloudflareZones(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	zones, err := s.Setup.Zones(r.Context())
	if err != nil {
		writeErr(w, 502, err)
		return
	}
	out := []zoneInfo{}
	for _, z := range zones {
		out = append(out, zoneInfo{z.Name, cfg.IsLocalDomain(z.Name)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	writeJSON(w, out)
}

type domainInfo struct {
	Name        string                   `json:"name"`
	Primary     bool                     `json:"primary"`
	AccountID   string                   `json:"account_id"`
	ConnectedAt time.Time                `json:"connected_at"`
	Accounts    int                      `json:"accounts"`
	Status      *cloudflare.DomainStatus `json:"status"`
}

func (s *Server) listDomains(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	checks := s.currentChecks()
	accts := s.Users.List()
	out := []domainInfo{}
	for i, d := range cfg.Domains {
		z := cfg.Cloudflare.Zones[d]
		di := domainInfo{Name: d, Primary: i == 0, AccountID: z.AccountID, ConnectedAt: z.ConnectedAt}
		for _, u := range accts {
			if strings.HasSuffix(u, "@"+d) {
				di.Accounts++
			}
		}
		if checks != nil {
			for _, ds := range checks.Cloudflare.Domains {
				if ds.Name == d {
					di.Status = &ds
				}
			}
		}
		out = append(out, di)
	}
	writeJSON(w, out)
}

func (s *Server) removeDomain(w http.ResponseWriter, r *http.Request) {
	var req struct{ Domain string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	warning, err := s.Setup.Disconnect(r.Context(), req.Domain)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	go s.refresh(context.Background())
	writeJSON(w, map[string]any{"ok": true, "warning": warning})
}

func (s *Server) tailscaleEnable(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	st := tailscale.GetStatus(r.Context(), cfg.Ports.IMAP, cfg.Ports.Submission)
	switch {
	case !st.Installed:
		writeJSON(w, map[string]any{"error": "Tailscale isn't installed on this machine.", "install": true})
		return
	case !st.Running:
		writeJSON(w, map[string]any{"error": "Sign in to Tailscale on this machine first.", "action_url": st.LoginURL})
		return
	}
	err := tailscale.EnableFunnel(r.Context(), cfg.Ports.IMAP, cfg.Ports.Submission, s.OpenURL)
	if err != nil {
		s.Log.Printf("tailscale: enabling Funnel: %v", err)
	}
	var an *tailscale.ActionNeeded
	if errors.As(err, &an) {
		writeJSON(w, map[string]any{"error": an.Message, "action_url": an.URL})
		return
	}
	if err != nil {
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	s.Cfg.Update(func(c *config.Config) {
		c.Tailscale.DNSName = st.DNSName
		c.Tailscale.EnabledAt = time.Now()
	})
	s.Log.Printf("tailscale: Funnel enabled on %s", st.DNSName)
	go s.refresh(context.Background())
	writeJSON(w, map[string]any{"ok": true, "dns_name": st.DNSName})
}

type mailboxInfo struct {
	Name     string `json:"name"`
	Messages int    `json:"messages"`
	Unseen   int    `json:"unseen"`
	Size     int64  `json:"size"`
}

type accountInfo struct {
	Email     string        `json:"email"`
	Mailboxes []mailboxInfo `json:"mailboxes"`
	Messages  int           `json:"messages"`
	Unseen    int           `json:"unseen"`
	Size      int64         `json:"size"`
	Aliases   []string      `json:"aliases"`
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	out := []accountInfo{}
	for _, email := range s.Users.List() {
		ai := accountInfo{Email: email, Aliases: []string{}}
		if acct, err := s.Store.Account(email); err == nil {
			for _, mb := range acct.List() {
				st, err := acct.Status(mb.Name)
				if err != nil {
					continue
				}
				ai.Mailboxes = append(ai.Mailboxes, mailboxInfo{mb.Name, st.Messages, st.Unseen, st.Size})
				ai.Messages += st.Messages
				ai.Size += st.Size
				if mb.Name == "INBOX" {
					ai.Unseen = st.Unseen
				}
			}
		}
		for alias, target := range cfg.Aliases {
			if target == email {
				ai.Aliases = append(ai.Aliases, alias)
			}
		}
		sort.Strings(ai.Aliases)
		out = append(out, ai)
	}
	writeJSON(w, out)
}

const pwAlphabet = "abcdefghjkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func generatePassword() string {
	b := make([]byte, 20)
	rand.Read(b)
	for i := range b {
		b[i] = pwAlphabet[int(b[i])%len(pwAlphabet)]
	}
	return string(b[:5]) + "-" + string(b[5:10]) + "-" + string(b[10:15]) + "-" + string(b[15:])
}

func passwordOr(pw string) string {
	if pw == "" {
		return generatePassword()
	}
	return pw
}

func (s *Server) normalizeLocal(input string) (string, error) {
	cfg := s.Cfg.Get()
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return "", errors.New("enter an address")
	}
	if !strings.Contains(input, "@") {
		if cfg.Domain() == "" {
			return "", errors.New("connect Cloudflare first so the domain is known")
		}
		input += "@" + cfg.Domain()
	}
	_, domain, _ := strings.Cut(input, "@")
	if !cfg.IsLocalDomain(domain) {
		return "", fmt.Errorf("%s isn't one of your domains", domain)
	}
	return users.Normalize(input)
}

// createAccount and resetPassword use the password given, or generate one
// when it's empty.
func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email, Password string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	email, err := s.normalizeLocal(req.Email)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	pw := passwordOr(req.Password)
	if err := s.Users.Add(email, pw); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Store.Account(email) // create default folders now
	s.Log.Printf("admin: created account %s", email)
	writeJSON(w, map[string]string{"email": email, "password": pw})
}

func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email, Password string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	pw := passwordOr(req.Password)
	if err := s.Users.SetPassword(req.Email, pw); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("admin: set password for %s", req.Email)
	writeJSON(w, map[string]string{"email": req.Email, "password": pw})
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.Users.Delete(req.Email); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("admin: deleted account %s (mail kept on disk)", req.Email)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) getAliases(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Cfg.Get().Aliases)
}

func (s *Server) putAliases(w http.ResponseWriter, r *http.Request) {
	var in map[string]string
	if err := readJSON(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	cfg := s.Cfg.Get()
	clean := map[string]string{}
	for alias, target := range in {
		alias = strings.ToLower(strings.TrimSpace(alias))
		target = strings.ToLower(strings.TrimSpace(target))
		if alias == "" {
			continue
		}
		if !strings.Contains(alias, "@") {
			alias += "@" + cfg.Domain()
		}
		_, domain, _ := strings.Cut(alias, "@")
		if !cfg.IsLocalDomain(domain) {
			writeErr(w, 400, fmt.Errorf("%s isn't one of your domains", alias))
			return
		}
		if !s.Users.Exists(target) {
			writeErr(w, 400, fmt.Errorf("%s: no account %q", alias, target))
			return
		}
		clean[alias] = target
	}
	if err := s.Cfg.Update(func(c *config.Config) { c.Aliases = clean }); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, clean)
}

func (s *Server) getQueue(w http.ResponseWriter, r *http.Request) {
	items, err := s.Queue.List()
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if items == nil {
		items = []*queue.Item{}
	}
	writeJSON(w, items)
}

func (s *Server) retryQueue(w http.ResponseWriter, r *http.Request) {
	if err := s.Queue.RetryAll(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// openURL opens a web link (Cloudflare, Tailscale) in the default browser.
func (s *Server) openURL(w http.ResponseWriter, r *http.Request) {
	var req struct{ URL string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if !strings.HasPrefix(req.URL, "https://") || s.OpenURL == nil {
		writeErr(w, 400, errors.New("only https links can be opened"))
		return
	}
	s.OpenURL(req.URL)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) getLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Logs.Lines(300))
}

type settingsView struct {
	RejectDMARCFailures bool  `json:"reject_dmarc_failures"`
	SaveSentCopy        bool  `json:"save_sent_copy"`
	SendViaCloudflare   bool  `json:"send_via_cloudflare"`
	MaxMessageMB        int64 `json:"max_message_mb"`
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	writeJSON(w, settingsView{cfg.RejectDMARCFailures, cfg.SaveSentCopy, cfg.SendViaCloudflare, cfg.MaxMessageBytes >> 20})
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var v settingsView
	if err := readJSON(r, &v); err != nil {
		writeErr(w, 400, err)
		return
	}
	if v.MaxMessageMB < 1 || v.MaxMessageMB > 25 {
		writeErr(w, 400, errors.New("max message size must be 1–25 MB (Cloudflare's limit is 25 MB)"))
		return
	}
	// Onboard every domain before switching sending over, so mail never
	// goes to Cloudflare for a domain it won't send for.
	if v.SendViaCloudflare && !s.Cfg.Get().SendViaCloudflare {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		if err := s.Setup.EnableSending(ctx); err != nil {
			writeErr(w, 400, fmt.Errorf("couldn't turn on Cloudflare Email Sending: %w", err))
			return
		}
		s.Log.Printf("admin: sending through Cloudflare Email Sending")
	}
	err := s.Cfg.Update(func(c *config.Config) {
		c.RejectDMARCFailures = v.RejectDMARCFailures
		c.SaveSentCopy = v.SaveSentCopy
		c.SendViaCloudflare = v.SendViaCloudflare
		c.MaxMessageBytes = v.MaxMessageMB << 20
	})
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, v)
}
