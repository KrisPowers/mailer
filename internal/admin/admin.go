// Package admin serves the web dashboard used to set up and manage the
// server. The container publishes it on the host's loopback only, and it
// can also be served through the Cloudflare Tunnel. Either way every API
// call but the sign-in's needs a signed-in account on an approved device.
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

	"mailer/internal/audit"
	"mailer/internal/auth"
	"mailer/internal/cloudflare"
	"mailer/internal/config"
	"mailer/internal/logbuf"
	"mailer/internal/maillog"
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
	// Sign-in attempts by mail apps, and the rate limit they count against.
	Audit   *audit.Log
	Limiter *users.Limiter
	// Auth holds the dashboard's own accounts, devices and sessions.
	Auth *auth.Store

	mu     sync.Mutex
	checks *Checks
	signIn *signIn
	limits limits
}

func (s *Server) Handler() http.Handler {
	s.limits = newLimits()
	mux := http.NewServeMux()
	web, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServerFS(web))

	// Before signing in: what the page needs to show the setup, a device
	// waiting for approval, or the sign-in.
	mux.HandleFunc("GET /api/auth/status", s.public(s.authStatus))
	mux.HandleFunc("POST /api/auth/device", s.public(s.newDevice))
	mux.HandleFunc("POST /api/auth/setup", s.public(s.firstRun))
	mux.HandleFunc("POST /api/auth/login", s.public(s.login))
	mux.HandleFunc("POST /api/auth/refresh", s.public(s.refreshToken))
	mux.HandleFunc("POST /api/auth/logout", s.public(s.logout))

	api := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.signedIn(h)) }
	api("GET /api/auth/me", s.me)
	api("POST /api/auth/password", s.changePassword)
	api("GET /api/auth/sessions", s.sessions)
	api("POST /api/auth/sessions/end", s.endSession)
	api("GET /api/auth/users", adminOnly(s.listUsers))
	api("POST /api/auth/users", adminOnly(s.addUser))
	api("POST /api/auth/users/password", adminOnly(s.setUserPassword))
	api("POST /api/auth/users/role", adminOnly(s.setUserRole))
	api("POST /api/auth/users/disable", adminOnly(s.setUserDisabled))
	api("POST /api/auth/users/delete", adminOnly(s.deleteUser))
	api("GET /api/auth/devices", adminOnly(s.listDevices))
	api("POST /api/auth/devices/approve", adminOnly(s.approveDevice))
	api("POST /api/auth/devices/rename", adminOnly(s.renameDevice))
	api("POST /api/auth/devices/delete", adminOnly(s.removeDevice))
	api("GET /api/server", adminOnly(s.getServer))
	api("PUT /api/server", adminOnly(s.putServer))

	api("GET /api/overview", s.overview)
	api("POST /api/check", s.runChecksNow)
	api("POST /api/cloudflare/signin", s.cloudflareSignIn)
	api("POST /api/cloudflare/signin/wait", s.cloudflareSignInWait)
	api("POST /api/cloudflare/connect", s.cloudflareConnect)
	api("POST /api/tailscale/enable", s.tailscaleEnable)
	api("GET /api/accounts", s.listAccounts)
	api("POST /api/accounts", s.createAccount)
	api("POST /api/accounts/password", s.resetPassword)
	api("POST /api/accounts/delete", s.deleteAccount)
	api("POST /api/accounts/locks", s.setLocks)
	api("POST /api/accounts/apps", s.setMailApps)
	api("GET /api/aliases", s.getAliases)
	api("PUT /api/aliases", s.putAliases)
	api("GET /api/flagged", s.getFlagged)
	api("POST /api/flagged/clear", s.clearFlags)
	api("POST /api/flagged/trash", s.trashFlagged)
	api("GET /api/queue", s.getQueue)
	api("GET /api/mail", s.getMail)
	api("POST /api/queue/retry", s.retryQueue)
	api("GET /api/logs", s.getLogs)
	api("GET /api/signins", s.getSignIns)
	api("POST /api/signins/unblock", s.unblock)
	api("POST /api/signins/test", s.testSignIn)
	api("GET /api/settings", s.getSettings)
	api("PUT /api/settings", s.putSettings)
	// Anything else under /api is refused, not answered with the page.
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, 404, errors.New("not found"))
	})
	return s.guard(mux)
}

// guard rejects requests for any name but a loopback one or the
// dashboard's public address (defeats DNS rebinding), and state-changing
// requests without a custom header, which other sites can't send
// cross-origin. Until the first admin exists, the API answers this
// computer only.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(hostOnly(r.Host))
		cfg := s.Cfg.Get()
		public := cfg.DashboardHost()
		if !loopbackName(host) && (public == "" || host != public) {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet && r.Header.Get("X-Mailer") != "1" {
			http.Error(w, "missing X-Mailer header", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; "+
			"frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("Cache-Control", "no-store")
		remote := !local(r)
		if remote {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if remote && strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/auth/status" && s.Auth.NeedsSetup() {
			writeErr(w, 403, errors.New("finish setting up mailer on the computer running it first"))
			return
		}
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
	wg.Add(2)
	go func() {
		defer wg.Done()
		c.Cloudflare = s.Setup.CheckStatus(ctx, probeHealth)
	}()
	go func() {
		defer wg.Done()
		c.Tailscale = tailscale.GetStatus(ctx, cfg.Ports.IMAP, cfg.Ports.Submission)
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
		add("tailscale", "Tailscale", false, "Not installed", "Run Mailer from its container image, which includes Tailscale.")
	case !ts.Running:
		add("tailscale", "Tailscale", false, "Not signed in ("+orDash(ts.State)+")",
			"Click Enable Funnel and sign in to Tailscale with the link it shows, or add a Tailscale auth key in Settings, Server.")
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
	// Outgoing mail only leaves through Cloudflare Email Sending.
	if cf.Connected && cf.Error == "" {
		var off []string
		for _, d := range cf.Domains {
			for _, ch := range d.Checks {
				if ch.Name == "Email Sending" && !ch.OK {
					off = append(off, d.Name+": "+ch.Detail)
				}
			}
		}
		detail := "outgoing mail leaves through Cloudflare"
		if len(off) > 0 {
			detail = strings.Join(off, "; ")
		}
		add("sending", "Cloudflare Email Sending", len(off) == 0, detail,
			"Outgoing mail waits in the queue until Cloudflare takes it. Email Sending needs the Workers Paid plan "+
				"on the Cloudflare account; once it's on, "+repair)
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
		return "-"
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
	Server     string `json:"server"`
	IMAPPort   int    `json:"imap_port"`
	SMTPServer string `json:"smtp_server"`
	SMTPPort   int    `json:"smtp_port"`
	// Set when sending goes through Cloudflare's relay, which takes its own
	// sign-in. Only signed-in dashboard accounts see it.
	SMTPUser     string `json:"smtp_user,omitempty"`
	SMTPPassword string `json:"smtp_password,omitempty"`
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
		cfg.Tailscale.DNSName = dnsName
		host, port, relay := cfg.SMTPServer()
		client = &clientSettings{Server: dnsName, IMAPPort: config.PublicIMAPPort, SMTPServer: host, SMTPPort: port}
		if relay {
			client.SMTPUser, client.SMTPPassword = config.RelayUser, cfg.Cloudflare.SMTPToken
		}
	}
	// Admins see how many devices are waiting for them.
	waiting := 0
	if claimsOf(r).Role == auth.RoleAdmin {
		for _, d := range s.Auth.Devices() {
			if !d.Approved {
				waiting++
			}
		}
	}
	writeJSON(w, map[string]any{
		"pending_devices":  waiting,
		"dashboard_host":   cfg.DashboardHost(),
		"webmail_host":     cfg.WebmailHost(),
		"webmail_port":     cfg.Ports.Webmail,
		"domain":           cfg.Domain(),
		"domains":          cfg.Domains,
		"hostname":         cfg.Hostname,
		"cloudflare_setup": cfg.Connected(),
		"tailscale_setup":  !cfg.Tailscale.EnabledAt.IsZero(),
		"accounts":         len(s.Users.List()),
		"queue":            len(q),
		"flagged":          s.flaggedCount(),
		"checks":           checks,
		"client":           client,
		"last_inbound":     s.MTA.LastInbound(),
		"connected_at":     cfg.Cloudflare.ConnectedAt,
		"funnel_at":        cfg.Tailscale.EnabledAt,
	})
}

func (s *Server) runChecksNow(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.refresh(r.Context()))
}

// signIn is a Cloudflare sign-in waiting for the browser to come back.
type signIn struct {
	cancel context.CancelFunc
	done   chan map[string]any // the outcome, once
	ended  chan struct{}       // closed when its callback listener is gone
}

// cloudflareSignIn is the one-click connect. It starts Cloudflare's
// sign-in and returns its page for the dashboard to open; once that page
// is approved the setup runs, and cloudflareSignInWait reports the outcome.
func (s *Server) cloudflareSignIn(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	prev := s.signIn
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	si := &signIn{cancel: cancel, done: make(chan map[string]any, 1), ended: make(chan struct{})}
	s.signIn = si
	s.mu.Unlock()
	if prev != nil {
		// Its listener holds the callback port.
		prev.cancel()
		<-prev.ended
	}

	page := make(chan string, 1)
	go func() {
		defer cancel()
		err := s.Setup.SignIn(ctx, func(u string) { page <- u })
		close(si.ended)
		if err != nil {
			si.done <- map[string]any{"error": err.Error()}
			return
		}
		si.done <- s.connect(ctx, "")
	}()
	select {
	case u := <-page:
		writeJSON(w, map[string]string{"url": u})
	case res := <-si.done:
		writeJSON(w, res)
	}
}

func (s *Server) cloudflareSignInWait(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	si := s.signIn
	s.mu.Unlock()
	if si == nil {
		writeErr(w, 400, errors.New("no Cloudflare sign-in in progress; click Connect Cloudflare again"))
		return
	}
	select {
	case res := <-si.done:
		writeJSON(w, res)
	case <-r.Context().Done():
	}
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
	writeJSON(w, s.connect(r.Context(), req.Zone))
}

// connect runs the Cloudflare setup for zone and describes the outcome for
// the dashboard.
func (s *Server) connect(ctx context.Context, zone string) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	steps, err := s.Setup.Connect(ctx, zone)
	var zc *cloudflare.ZoneChoiceError
	if errors.As(err, &zc) {
		return map[string]any{"choose_zone": zc.Zones}
	}
	resp := map[string]any{"steps": steps}
	if err != nil {
		resp["error"] = err.Error()
	}
	go s.refresh(context.Background())
	return resp
}

func (s *Server) tailscaleEnable(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	st := tailscale.GetStatus(r.Context(), cfg.Ports.IMAP, cfg.Ports.Submission)
	switch {
	case !st.Installed:
		writeJSON(w, map[string]any{"error": "Tailscale isn't installed. Run Mailer from its container image, which includes it."})
		return
	case !st.Running && st.LoginURL != "":
		writeJSON(w, map[string]any{"error": "Sign in to Tailscale to add this container to your tailnet, then click Enable again.",
			"action_url": st.LoginURL})
		return
	case !st.Running:
		writeJSON(w, map[string]any{"error": "Tailscale isn't signed in and has no sign-in link yet (" + orDash(st.State) +
			"). Wait a few seconds and click Enable again, or add a Tailscale auth key in Settings, Server."})
		return
	}
	err := tailscale.EnableFunnel(r.Context(), cfg.Ports.IMAP, cfg.Ports.Submission)
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
	Locks     users.Locks   `json:"locks"`
	// MailApps is whether Outlook and other mail apps may sign in.
	MailApps bool `json:"mail_apps"`
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	out := []accountInfo{}
	for _, email := range s.Users.List() {
		ai := accountInfo{Email: email, Aliases: []string{}, Locks: s.Users.Locks(email), MailApps: s.Users.MailApps(email)}
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
	var req struct {
		Email, Password string
		MailApps        bool `json:"mail_apps"`
	}
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
	if req.MailApps {
		if err := s.Users.SetMailApps(email, true); err != nil {
			writeErr(w, 500, err)
			return
		}
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

// setLocks turns an account's receive, send and sign-in locks on or off.
func (s *Server) setLocks(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string
		users.Locks
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.Users.SetLocks(req.Email, req.Locks); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("admin: locks for %s: receive=%v send=%v sign-in=%v", req.Email, req.Receive, req.Send, req.Login)
	writeJSON(w, req.Locks)
}

// setMailApps lets an account's mail apps sign in, or stops them; the
// webmail works either way.
func (s *Server) setMailApps(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
		On    bool   `json:"on"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.Users.SetMailApps(req.Email, req.On); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("admin: mail apps for %s: %v", req.Email, req.On)
	writeJSON(w, map[string]bool{"on": req.On})
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

// getMail returns what happened to recent messages, received and sent.
func (s *Server) getMail(w http.ResponseWriter, r *http.Request) {
	entries := s.MTA.Mail.Recent()
	if entries == nil {
		entries = []maillog.Entry{}
	}
	writeJSON(w, entries)
}

func (s *Server) retryQueue(w http.ResponseWriter, r *http.Request) {
	if err := s.Queue.RetryAll(); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) getLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Logs.Lines(300))
}

type settingsView struct {
	RejectDMARCFailures bool   `json:"reject_dmarc_failures"`
	SaveSentCopy        bool   `json:"save_sent_copy"`
	MaxMessageMB        int64  `json:"max_message_mb"`
	SMTPToken           string `json:"smtp_token"`
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	cfg := s.Cfg.Get()
	writeJSON(w, settingsView{cfg.RejectDMARCFailures, cfg.SaveSentCopy, cfg.MaxMessageBytes >> 20, cfg.Cloudflare.SMTPToken})
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
	v.SMTPToken = strings.TrimSpace(v.SMTPToken)
	if v.SMTPToken != "" && v.SMTPToken != s.Cfg.Get().Cloudflare.SMTPToken {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := smtpSignIn(ctx, config.RelayHost, config.RelayPort, config.RelayUser, v.SMTPToken); err != nil {
			writeErr(w, 400, fmt.Errorf("Cloudflare's SMTP relay refused the token: %w", err))
			return
		}
		s.Log.Printf("admin: mail apps now send through Cloudflare's SMTP relay")
	}
	err := s.Cfg.Update(func(c *config.Config) {
		c.RejectDMARCFailures = v.RejectDMARCFailures
		c.SaveSentCopy = v.SaveSentCopy
		c.Cloudflare.SMTPToken = v.SMTPToken
		c.MaxMessageBytes = v.MaxMessageMB << 20
	})
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, v)
}
