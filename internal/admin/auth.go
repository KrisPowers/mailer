package admin

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"mailer/internal/auth"
	"mailer/internal/config"
	"mailer/internal/tailscale"
)

const (
	// deviceHeader carries the ID the browser keeps in localStorage.
	deviceHeader = "X-Device"
	// refreshCookie holds the refresh token, out of the page's reach.
	refreshCookie = "mailer_refresh"
)

type claimsKey struct{}

func claimsOf(r *http.Request) auth.Claims {
	c, _ := r.Context().Value(claimsKey{}).(auth.Claims)
	return c
}

// limits are the dashboard's rate limits, per visitor address and, for
// failed sign-ins, per username too.
type limits struct {
	requests *auth.Window // public auth calls
	devices  *auth.Window // new device IDs
	failures *auth.Window // wrong passwords
}

func newLimits() limits {
	return limits{
		requests: &auth.Window{Max: 60, Per: time.Minute},
		devices:  &auth.Window{Max: 10, Per: time.Hour},
		failures: &auth.Window{Max: 5, Per: 15 * time.Minute},
	}
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

// local reports whether r came from a browser on this computer. Requests
// through the tunnel carry the dashboard's public name and Cloudflare's
// headers; the port is only published on this computer's loopback.
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

// signedIn lets a request through only with a valid token from the device
// it was issued to, or with the hub's key.
func (s *Server) signedIn(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, ok := s.fromHub(r); ok {
			next(w, r.WithContext(context.WithValue(r.Context(), claimsKey{}, c)))
			return
		}
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			writeErr(w, 401, auth.ErrSession)
			return
		}
		c, err := s.Auth.Verify(token, r.Header.Get(deviceHeader))
		if err != nil {
			writeErr(w, 401, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), claimsKey{}, c)))
	})
}

func adminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if claimsOf(r).Role != auth.RoleAdmin {
			writeErr(w, 403, errors.New("only an admin can do this"))
			return
		}
		next(w, r)
	}
}

// public rate-limits the calls made before signing in.
func (s *Server) public(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.limits.requests.Allow(clientIP(r)) {
			writeErr(w, 429, errors.New("too many requests, wait a minute"))
			return
		}
		next(w, r)
	}
}

func (s *Server) setRefresh(w http.ResponseWriter, r *http.Request, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: refreshCookie, Value: value, Path: "/api/auth", MaxAge: maxAge,
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: !local(r)})
}

func (s *Server) signedInAs(w http.ResponseWriter, r *http.Request, t auth.Tokens) {
	if t.Refresh != "" {
		s.setRefresh(w, r, t.Refresh, int((30 * 24 * time.Hour).Seconds()))
	}
	writeJSON(w, t)
}

func refreshOf(r *http.Request) string {
	c, err := r.Cookie(refreshCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

// ---- before signing in ----

// authStatus tells the page what to show: the setup, a device waiting for
// approval, or the sign-in.
func (s *Server) authStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{"setup": s.Auth.NeedsSetup(), "local": local(r), "device": nil}
	if d, ok := s.Auth.Device(r.Header.Get(deviceHeader)); ok {
		resp["device"] = map[string]any{"code": d.Code, "approved": d.Approved}
	}
	writeJSON(w, resp)
}

// newDevice gives a browser its device ID on its first visit.
func (s *Server) newDevice(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.limits.devices.Allow(ip) {
		writeErr(w, 429, errors.New("too many new devices from this address, try again later"))
		return
	}
	id, d, err := s.Auth.NewDevice(r.UserAgent(), ip)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	s.Log.Printf("auth: new device %s from %s is waiting for approval", d.Code, ip)
	writeJSON(w, map[string]any{"id": id, "code": d.Code, "approved": false})
}

var labelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func checkLabel(what, v string) (string, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if !labelRe.MatchString(v) {
		return "", fmt.Errorf("%s must be letters, digits and dashes, up to 63 characters", what)
	}
	return v, nil
}

// firstRun is the setup, from this computer only: the first admin, the
// device they're on, and the container settings.
func (s *Server) firstRun(w http.ResponseWriter, r *http.Request) {
	if !local(r) {
		writeErr(w, 403, errors.New("setup can only be done on the computer running mailer"))
		return
	}
	var req struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		Tailscale struct {
			Hostname string `json:"hostname"`
			AuthKey  string `json:"auth_key"`
		} `json:"tailscale"`
		Remote config.Remote `json:"remote"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	host, err := checkLabel("The Tailscale name", req.Tailscale.Hostname)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	sub, err := checkLabel("The dashboard's subdomain", req.Remote.Subdomain)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	key := strings.TrimSpace(req.Tailscale.AuthKey)
	t, err := s.Auth.Setup(req.Username, req.Password, r.Header.Get(deviceHeader), clientIP(r), r.UserAgent())
	if errors.Is(err, auth.ErrSetupDone) {
		writeErr(w, 409, err)
		return
	}
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.Cfg.Update(func(c *config.Config) {
		c.Tailscale.Hostname, c.Tailscale.AuthKey = host, key
		c.Remote = config.Remote{Enabled: req.Remote.Enabled, Subdomain: sub}
	}); err != nil {
		writeErr(w, 500, err)
		return
	}
	s.Log.Printf("auth: setup done, admin %s created", t.User.Name)
	tailscale.Up(context.Background(), host, key, s.Log.Printf)
	go s.syncRoutes()
	s.signedInAs(w, r, t)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	ip := clientIP(r)
	name := strings.ToLower(strings.TrimSpace(req.Username))
	ipKey, userKey := "ip:"+ip, "user:"+name
	for _, k := range []string{ipKey, userKey} {
		if until := s.limits.failures.Until(k); !until.IsZero() {
			writeErr(w, 429, fmt.Errorf("too many wrong passwords, try again in %d minutes", int(time.Until(until).Minutes())+1))
			return
		}
	}
	t, err := s.Auth.Login(name, req.Password, r.Header.Get(deviceHeader), ip, r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrDevice):
		writeErr(w, 403, err)
		return
	case errors.Is(err, auth.ErrCredentials):
		s.limits.failures.Add(ipKey)
		s.limits.failures.Add(userKey)
		s.Log.Printf("auth: wrong password for %q from %s", name, ip)
		writeErr(w, 401, err)
		return
	case err != nil:
		writeErr(w, 500, err)
		return
	}
	s.limits.failures.Reset(ipKey)
	s.limits.failures.Reset(userKey)
	s.Log.Printf("auth: %s signed in from %s", name, ip)
	s.signedInAs(w, r, t)
}

func (s *Server) refreshToken(w http.ResponseWriter, r *http.Request) {
	t, err := s.Auth.Refresh(refreshOf(r), r.Header.Get(deviceHeader), clientIP(r))
	if err != nil {
		s.setRefresh(w, r, "", -1)
		writeErr(w, 401, err)
		return
	}
	s.signedInAs(w, r, t)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.Auth.Logout(refreshOf(r))
	s.setRefresh(w, r, "", -1)
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- signed in ----

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	u, _ := s.Auth.User(c.Subject)
	d, _ := s.Auth.Device(r.Header.Get(deviceHeader))
	writeJSON(w, map[string]any{"user": u, "session": c.Session, "device": d.Code})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	c := claimsOf(r)
	userKey := "user:" + c.Subject
	if !s.limits.failures.Until(userKey).IsZero() {
		writeErr(w, 429, errors.New("too many wrong passwords, try again later"))
		return
	}
	if !s.Auth.CheckPassword(c.Subject, req.Current) {
		s.limits.failures.Add(userKey)
		writeErr(w, 400, errors.New("the current password is wrong"))
		return
	}
	if err := s.Auth.SetPassword(c.Subject, req.Password, c.Session); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("auth: %s changed their password", c.Subject)
	writeJSON(w, map[string]bool{"ok": true})
}

// sessions lists the caller's sessions, or everyone's for an admin.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	c := claimsOf(r)
	owner := c.Subject
	if c.Role == auth.RoleAdmin {
		owner = ""
	}
	writeJSON(w, map[string]any{"current": c.Session, "sessions": s.Auth.Sessions(owner)})
}

func (s *Server) endSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	c := claimsOf(r)
	owner := c.Subject
	if c.Role == auth.RoleAdmin {
		owner = ""
	}
	if err := s.Auth.EndSession(req.ID, owner); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- admin: accounts ----

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Auth.Users())
}

// addUser and setUserPassword use the password given, or generate one
// when it's empty.
func (s *Server) addUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	pw := passwordOr(req.Password)
	u, err := s.Auth.AddUser(req.Username, pw, req.Role, claimsOf(r).Subject)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("auth: %s created dashboard account %s (%s)", claimsOf(r).Subject, u.Name, u.Role)
	writeJSON(w, map[string]any{"user": u, "password": pw})
}

type userReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
}

// userAction reads a request about another account; an admin can't lock
// themselves out from here.
func userAction(w http.ResponseWriter, r *http.Request) (userReq, bool) {
	var req userReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return req, false
	}
	if req.Username == claimsOf(r).Subject {
		writeErr(w, 400, errors.New("that's your own account; change your password under Security"))
		return req, false
	}
	return req, true
}

func (s *Server) setUserPassword(w http.ResponseWriter, r *http.Request) {
	req, ok := userAction(w, r)
	if !ok {
		return
	}
	pw := passwordOr(req.Password)
	if err := s.Auth.SetPassword(req.Username, pw, ""); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("auth: %s set the password for %s", claimsOf(r).Subject, req.Username)
	writeJSON(w, map[string]string{"username": req.Username, "password": pw})
}

func (s *Server) setUserRole(w http.ResponseWriter, r *http.Request) {
	req, ok := userAction(w, r)
	if !ok {
		return
	}
	if err := s.Auth.SetRole(req.Username, req.Role); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("auth: %s made %s %s", claimsOf(r).Subject, req.Username, req.Role)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) setUserDisabled(w http.ResponseWriter, r *http.Request) {
	req, ok := userAction(w, r)
	if !ok {
		return
	}
	if err := s.Auth.SetDisabled(req.Username, req.Disabled); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("auth: %s turned sign-in %s for %s", claimsOf(r).Subject, map[bool]string{true: "off", false: "on"}[req.Disabled], req.Username)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	req, ok := userAction(w, r)
	if !ok {
		return
	}
	if err := s.Auth.DeleteUser(req.Username); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("auth: %s deleted dashboard account %s", claimsOf(r).Subject, req.Username)
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- admin: devices ----

func (s *Server) listDevices(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"current": claimsOf(r).Device, "devices": s.Auth.Devices()})
}

type deviceReq struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

func (s *Server) approveDevice(w http.ResponseWriter, r *http.Request) {
	var req deviceReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.Auth.Approve(req.Key, req.Name, claimsOf(r).Subject); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("auth: %s approved a device (%s)", claimsOf(r).Subject, req.Name)
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) renameDevice(w http.ResponseWriter, r *http.Request) {
	var req deviceReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.Auth.RenameDevice(req.Key, req.Name); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) removeDevice(w http.ResponseWriter, r *http.Request) {
	var req deviceReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Key == claimsOf(r).Device {
		writeErr(w, 400, errors.New("that's the device you're using"))
		return
	}
	if err := s.Auth.RemoveDevice(req.Key); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("auth: %s removed a device", claimsOf(r).Subject)
	writeJSON(w, map[string]bool{"ok": true})
}

// ---- admin: container settings ----

// serverView is what used to be the container's environment: the
// Tailscale name and key, remote access and the debugging switches. The
// auth key is never sent back, only whether one is set.
type serverView struct {
	TailscaleHostname string  `json:"tailscale_hostname"`
	AuthKeySet        bool    `json:"auth_key_set"`
	AuthKey           *string `json:"auth_key,omitempty"` // in a PUT: nil keeps it, "" clears it
	RemoteEnabled     bool    `json:"remote_enabled"`
	RemoteSubdomain   string  `json:"remote_subdomain"`
	DashboardHost     string  `json:"dashboard_host"`
	Trace             bool    `json:"trace"`
	IMAPCaps          string  `json:"imap_caps"`
	Warning           string  `json:"warning,omitempty"`
}

func viewOf(c config.Config) serverView {
	host := c.DashboardHost()
	if host == "" && c.Domain() != "" {
		host = c.Remote.Subdomain + "." + c.Domain()
	}
	return serverView{TailscaleHostname: c.Tailscale.Hostname, AuthKeySet: c.Tailscale.AuthKey != "",
		RemoteEnabled: c.Remote.Enabled, RemoteSubdomain: c.Remote.Subdomain, DashboardHost: host,
		Trace: c.Trace, IMAPCaps: c.IMAPCaps}
}

func (s *Server) getServer(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, viewOf(s.Cfg.Get()))
}

func (s *Server) putServer(w http.ResponseWriter, r *http.Request) {
	var v serverView
	if err := readJSON(r, &v); err != nil {
		writeErr(w, 400, err)
		return
	}
	host, err := checkLabel("The Tailscale name", v.TailscaleHostname)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	sub, err := checkLabel("The dashboard's subdomain", v.RemoteSubdomain)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	before := s.Cfg.Get()
	if err := s.Cfg.Update(func(c *config.Config) {
		c.Tailscale.Hostname = host
		if v.AuthKey != nil {
			c.Tailscale.AuthKey = strings.TrimSpace(*v.AuthKey)
		}
		c.Remote = config.Remote{Enabled: v.RemoteEnabled, Subdomain: sub}
		c.Trace = v.Trace
		c.IMAPCaps = strings.Join(strings.Fields(v.IMAPCaps), " ")
	}); err != nil {
		writeErr(w, 500, err)
		return
	}
	after := s.Cfg.Get()
	who := claimsOf(r).Subject
	if after.Tailscale.Hostname != before.Tailscale.Hostname || after.Tailscale.AuthKey != before.Tailscale.AuthKey {
		s.Log.Printf("admin: %s changed the Tailscale settings", who)
		tailscale.Up(context.Background(), after.Tailscale.Hostname, after.Tailscale.AuthKey, s.Log.Printf)
	}
	out := viewOf(after)
	if after.DashboardHost() != before.DashboardHost() {
		s.Log.Printf("admin: %s set remote dashboard access to %q", who, after.DashboardHost())
		switch {
		case !after.Connected():
			if after.Remote.Enabled {
				out.Warning = "Remote access starts once Cloudflare is connected."
			}
		default:
			ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
			defer cancel()
			if err := s.Setup.SyncRoutes(ctx); err != nil {
				out.Warning = "Saved, but Cloudflare's routes couldn't be updated yet: " + err.Error()
			}
		}
	}
	writeJSON(w, out)
}

// syncRoutes applies the tunnel's routes in the background.
func (s *Server) syncRoutes() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := s.Setup.SyncRoutes(ctx); err != nil {
		s.Log.Printf("cloudflare: updating the tunnel's routes: %v", err)
	}
}
