// Package webmail serves the mail app in the browser, beside the admin
// dashboard: folders, reading, writing, signatures, categories, flags and
// rules, for the accounts the dashboard makes, so no other mail app is
// needed. It listens on its own port, published on the host's loopback,
// and is served through the Cloudflare Tunnel when the admin turns that on.
// Every call but the sign-in's needs a session, which the account's address
// and password start.
package webmail

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mailer/internal/audit"
	"mailer/internal/auth"
	"mailer/internal/config"
	"mailer/internal/mta"
	"mailer/internal/store"
	"mailer/internal/users"
)

//go:embed web
var webFS embed.FS

type Server struct {
	Cfg   *config.Manager
	Users *users.DB
	Store *store.Store
	MTA   *mta.MTA
	// Sign-ins count against the same limits as mail apps', and are
	// recorded beside theirs.
	Limiter *users.Limiter
	Audit   *audit.Log
	Log     *log.Logger
	// Dir holds each account's webmail settings and the open sessions.
	Dir string

	once     sync.Once
	sessions *sessions
	prefs    *prefsStore
	cache    *cache
	requests *auth.Window
}

func (s *Server) init() {
	s.once.Do(func() {
		s.sessions = openSessions(filepath.Join(s.Dir, "sessions.json"))
		s.prefs = &prefsStore{dir: s.Dir, m: map[string]*Prefs{}}
		s.cache = &cache{m: map[string]*parsed{}}
		s.requests = &auth.Window{Max: 30, Per: time.Minute}
	})
}

// account is the signed-in mailbox a request acts on.
type account struct {
	email string
	box   *store.Account
}

type handler func(http.ResponseWriter, *http.Request, *account)

func (s *Server) Handler() http.Handler {
	s.init()
	mux := http.NewServeMux()
	web, _ := fs.Sub(webFS, "web")
	mux.Handle("GET /", http.FileServerFS(web))

	mux.HandleFunc("GET /api/session", s.whoami)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)

	api := func(pattern string, h handler) { mux.HandleFunc(pattern, s.signedIn(h)) }
	api("GET /api/me", s.me)
	api("PUT /api/prefs", s.putPrefs)
	api("GET /api/folders", s.folders)
	api("POST /api/folders", s.createFolder)
	api("POST /api/folders/rename", s.renameFolder)
	api("POST /api/folders/delete", s.deleteFolder)
	api("POST /api/folders/empty", s.emptyFolder)
	api("GET /api/messages", s.messages)
	api("GET /api/message", s.message)
	api("GET /api/message/body", s.body)
	api("GET /api/message/part", s.part)
	api("GET /api/message/raw", s.raw)
	api("POST /api/messages/flags", s.setFlags)
	api("POST /api/messages/move", s.move)
	api("POST /api/messages/delete", s.deleteMessages)
	api("POST /api/send", s.send)
	api("POST /api/drafts", s.saveDraft)
	api("POST /api/rules/run", s.runRules)
	api("GET /api/contacts", s.contacts)
	// Anything else under /api is refused, not answered with the page.
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, 404, errors.New("not found"))
	})
	return s.guard(mux)
}

// guard rejects requests for any name but a loopback one or the webmail's
// public address (defeats DNS rebinding), and state-changing requests
// without a custom header, which other sites can't send cross-origin.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(hostOnly(r.Host))
		cfg := s.Cfg.Get()
		public := cfg.WebmailHost()
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
			"img-src 'self' data: blob:; frame-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("Cache-Control", "no-store")
		if !local(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		next.ServeHTTP(w, r)
	})
}

var errSignedOut = errors.New("signed out")

// signedIn lets a request through with a live session whose account still
// exists, has the same password and isn't locked out.
func (s *Server) signedIn(next handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a, err := s.account(r)
		if err != nil {
			writeErr(w, 401, err)
			return
		}
		next(w, r, a)
	}
}

func (s *Server) account(r *http.Request) (*account, error) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil, errSignedOut
	}
	se, ok := s.sessions.get(c.Value)
	if !ok {
		return nil, errSignedOut
	}
	if s.Users.Stamp(se.Email) != se.Stamp || s.Users.LoginLocked(se.Email) {
		s.sessions.end(c.Value)
		return nil, errSignedOut
	}
	box, err := s.Store.Account(se.Email)
	if err != nil {
		return nil, err
	}
	return &account{email: se.Email, box: box}, nil
}

const cookieName = "webmail"

func (s *Server) setCookie(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: !local(r)})
}

// whoami tells the page whether it's signed in, without counting as a
// failure when it isn't.
func (s *Server) whoami(w http.ResponseWriter, r *http.Request) {
	a, err := s.account(r)
	if err != nil {
		writeJSON(w, map[string]any{"signed_in": false})
		return
	}
	writeJSON(w, map[string]any{"signed_in": true, "email": a.email})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.requests.Allow(ip) {
		writeErr(w, 429, errors.New("too many requests, wait a minute"))
		return
	}
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req, 1<<16); err != nil {
		writeErr(w, 400, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	ev := audit.Event{Proto: "web", IP: ip, Client: agent(r), User: clip(email, 200)}
	refuse := func(code int, result, detail, msg string) {
		ev.Result, ev.Detail = result, detail
		s.Audit.Add(ev)
		writeErr(w, code, errors.New(msg))
	}
	if s.Limiter.Blocked(ip) {
		refuse(429, audit.Blocked, "too many failed sign-ins from this address in the last 15 minutes", "Too many failed sign-ins. Try again in 15 minutes.")
		return
	}
	if s.Limiter.AccountBlocked(email, ip) {
		refuse(429, audit.Blocked, "too many failed sign-ins to this account in the last 15 minutes", "Too many failed sign-ins. Try again in 15 minutes.")
		return
	}
	got, ok := s.Users.Authenticate(email, req.Password)
	if !ok {
		if s.Users.LoginLocked(email) {
			refuse(401, audit.Locked, "sign-in is locked for this account", "Wrong address or password.")
			return
		}
		result, detail := audit.BadPassword, "wrong password"
		if !s.Users.Exists(email) {
			result, detail = audit.UnknownUser, "no account with this address"
		} else {
			s.Limiter.AccountFail(email)
		}
		if s.Limiter.Fail(ip) {
			detail += "; this address is now blocked for 15 minutes"
		}
		s.Log.Printf("webmail: failed sign-in for %q from %s (%s)", email, ip, detail)
		refuse(401, result, detail, "Wrong address or password.")
		return
	}
	s.Limiter.Succeeded(got, ip)
	ev.Result, ev.User = audit.OK, got
	s.Audit.Add(ev)
	token, err := s.sessions.add(got, s.Users.Stamp(got), ip, agent(r))
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	s.setCookie(w, r, token, int(sessionLife.Seconds()))
	writeJSON(w, map[string]any{"signed_in": true, "email": got})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.sessions.end(c.Value)
	}
	s.setCookie(w, r, "", -1)
	writeJSON(w, map[string]bool{"ok": true})
}

// me is everything the page needs about the account: its addresses, its
// settings, and what it may do.
func (s *Server) me(w http.ResponseWriter, r *http.Request, a *account) {
	cfg := s.Cfg.Get()
	writeJSON(w, map[string]any{
		"email":     a.email,
		"addresses": s.addresses(a.email),
		"prefs":     s.prefs.get(a.email),
		"can_send":  !s.Users.Locks(a.email).Send,
		"max_bytes": cfg.MaxMessageBytes,
		"mail_apps": s.Users.MailApps(a.email),
	})
}

// addresses are the ones the account can send as: its own, then every
// alias that delivers to it. A catch-all doesn't count.
func (s *Server) addresses(email string) []string {
	out := []string{email}
	var more []string
	for alias, target := range s.Cfg.Get().Aliases {
		if target == email && !strings.HasPrefix(alias, "@") {
			more = append(more, alias)
		}
	}
	sort.Strings(more)
	return append(out, more...)
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

func readJSON(r *http.Request, v any, limit int64) error {
	return json.NewDecoder(io.LimitReader(r.Body, limit)).Decode(v)
}

func loopbackName(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	}
	return false
}

func hostOnly(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
}

// local reports whether r came from a browser on this computer rather than
// through the tunnel, which carries the public name and Cloudflare's headers.
func local(r *http.Request) bool {
	return loopbackName(hostOnly(r.Host)) && r.Header.Get("Cf-Connecting-Ip") == "" && r.Header.Get("Cf-Ray") == ""
}

// clientIP is the visitor's address. cloudflared connects from inside the
// container, so for tunnel requests it's the address Cloudflare saw.
func clientIP(r *http.Request) string {
	host := hostOnly(r.RemoteAddr)
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		if cf := r.Header.Get("Cf-Connecting-Ip"); cf != "" {
			return cf
		}
	}
	return host
}

func agent(r *http.Request) string { return clip(r.UserAgent(), 200) }

// clip cuts s to n bytes, without control characters.
func clip(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > n {
		s = s[:n]
	}
	return strings.ToValidUTF8(s, "")
}
