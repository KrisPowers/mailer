package admin

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mailer/internal/auth"
	"mailer/internal/cloudflare"
	"mailer/internal/config"
	"mailer/internal/logbuf"
	"mailer/internal/mta"
	"mailer/internal/queue"
	"mailer/internal/store"
	"mailer/internal/tunnel"
	"mailer/internal/users"
)

func newServer(t *testing.T) *Server {
	dir := t.TempDir()
	cfgm, _ := config.Open(dir)
	cfgm.Update(func(c *config.Config) { c.Domains = []string{"example.com"} })
	db, _ := users.Open(filepath.Join(dir, "users.json"))
	st := store.New(filepath.Join(dir, "mail"))
	logger := log.New(io.Discard, "", 0)
	q, _ := queue.New(filepath.Join(dir, "queue"), nil, nil, logger)
	m := &mta.MTA{Cfg: cfgm, Users: db, Store: st, Queue: q, Log: logger}
	a, err := auth.Open(filepath.Join(dir, "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	return &Server{Cfg: cfgm, Users: db, Store: st, Queue: q, MTA: m,
		Setup: &cloudflare.Setup{Cfg: cfgm, Log: logger}, Tunnel: &tunnel.Runner{Log: logger},
		Logs: logbuf.New(10), Log: logger, Auth: a}
}

// signInAdmin makes the first admin on a new device and returns the
// headers a signed-in page sends.
func signInAdmin(t *testing.T, s *Server) map[string]string {
	id, _, _ := s.Auth.NewDevice("test", "127.0.0.1")
	tok, err := s.Auth.Setup("admin", "correct horse battery", id, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{"X-Mailer": "1", "X-Device": id, "Authorization": "Bearer " + tok.Access}
}

func do(t *testing.T, h http.Handler, method, path, body string, hdr map[string]string) (int, map[string]any) {
	req := httptest.NewRequest(method, "http://127.0.0.1:8080"+path, strings.NewReader(body))
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var out map[string]any
	json.Unmarshal(rr.Body.Bytes(), &out)
	return rr.Code, out
}

func TestGuard(t *testing.T) {
	h := newServer(t).Handler()

	// DNS rebinding: a foreign Host header is refused.
	req := httptest.NewRequest("GET", "http://evil.example/api/overview", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Errorf("foreign host: %d", rr.Code)
	}
	// CSRF: a form post without the custom header is refused.
	if code, _ := do(t, h, "POST", "/api/accounts", `{"email":"x"}`, nil); code != 403 {
		t.Errorf("missing X-Mailer: %d", code)
	}
	if code, _ := do(t, h, "GET", "/", "", nil); code != 200 {
		t.Errorf("index: %d", code)
	}
	// The API needs a signed-in account.
	if code, _ := do(t, h, "GET", "/api/overview", "", nil); code != 401 {
		t.Errorf("overview without a token: %d", code)
	}
}

func TestAccounts(t *testing.T) {
	s := newServer(t)
	h := s.Handler()
	hdr := signInAdmin(t, s)

	code, out := do(t, h, "POST", "/api/accounts", `{"email":"kris"}`, hdr)
	if code != 200 || out["email"] != "kris@example.com" {
		t.Fatalf("create: %d %v", code, out)
	}
	pw := out["password"].(string)
	if _, ok := s.Users.Authenticate("kris@example.com", pw); !ok {
		t.Error("generated password doesn't work")
	}
	if code, _ := do(t, h, "POST", "/api/accounts", `{"email":"x@other.com"}`, hdr); code != 400 {
		t.Errorf("foreign domain accepted: %d", code)
	}

	if code, out := do(t, h, "PUT", "/api/aliases", `{"hello":"kris@example.com","@example.com":"kris@example.com"}`, hdr); code != 200 {
		t.Fatalf("aliases: %d %v", code, out)
	}
	if u, ok := s.MTA.Resolve("anything@example.com"); !ok || u != "kris@example.com" {
		t.Error("catch-all not applied")
	}
	if code, _ := do(t, h, "PUT", "/api/aliases", `{"x":"ghost@example.com"}`, hdr); code != 400 {
		t.Error("alias to missing account accepted")
	}

	req := httptest.NewRequest("GET", "http://localhost:8080/api/accounts", nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var list []accountInfo
	json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list) != 1 || len(list[0].Mailboxes) != 6 || len(list[0].Aliases) != 2 {
		t.Errorf("accounts list: %+v", list)
	}

	// Locking sign-in stops the password working; unlocking restores it.
	if code, _ := do(t, h, "POST", "/api/accounts/locks", `{"email":"kris@example.com","lock_login":true,"lock_send":true}`, hdr); code != 200 {
		t.Fatalf("locks: %d", code)
	}
	if l := s.Users.Locks("kris@example.com"); !l.Login || !l.Send || l.Receive {
		t.Errorf("locks saved as %+v", l)
	}
	if _, ok := s.Users.Authenticate("kris@example.com", pw); ok || !s.Users.LoginLocked("kris@example.com") {
		t.Error("locked account signed in")
	}
	do(t, h, "POST", "/api/accounts/locks", `{"email":"kris@example.com"}`, hdr)
	if _, ok := s.Users.Authenticate("kris@example.com", pw); !ok {
		t.Error("unlocked account can't sign in")
	}

	code, out = do(t, h, "POST", "/api/accounts/password", `{"email":"kris@example.com"}`, hdr)
	if code != 200 || out["password"] == pw {
		t.Errorf("reset: %d %v", code, out)
	}
	if code, _ := do(t, h, "POST", "/api/accounts/delete", `{"email":"kris@example.com"}`, hdr); code != 200 || s.Users.Exists("kris@example.com") {
		t.Error("delete failed")
	}
}

func TestDashboardAuth(t *testing.T) {
	s := newServer(t)
	h := s.Handler()
	here := map[string]string{"X-Mailer": "1"}

	// A first visit gets a device ID, waiting for approval.
	code, out := do(t, h, "POST", "/api/auth/device", "", here)
	if code != 200 || out["id"] == "" || out["approved"] != false {
		t.Fatalf("new device: %d %v", code, out)
	}
	first := out["id"].(string)

	// Setup is refused through the tunnel, and works here once.
	setup := `{"username":"kris","password":"correct horse battery","tailscale":{"hostname":"mail"},"remote":{"subdomain":"mail-admin"}}`
	tunnel := map[string]string{"X-Mailer": "1", "X-Device": first, "Cf-Connecting-Ip": "203.0.113.9"}
	if code, _ := do(t, h, "POST", "/api/auth/setup", setup, tunnel); code != 403 {
		t.Errorf("setup through the tunnel: %d", code)
	}
	code, out = do(t, h, "POST", "/api/auth/setup", setup, map[string]string{"X-Mailer": "1", "X-Device": first})
	if code != 200 || out["token"] == "" {
		t.Fatalf("setup: %d %v", code, out)
	}
	if code, _ := do(t, h, "POST", "/api/auth/setup", setup, map[string]string{"X-Mailer": "1", "X-Device": first}); code != 409 {
		t.Errorf("second setup: %d", code)
	}
	admin := map[string]string{"X-Mailer": "1", "X-Device": first, "Authorization": "Bearer " + out["token"].(string)}

	// A second device can't sign in until an admin approves it.
	_, out = do(t, h, "POST", "/api/auth/device", "", here)
	second := out["id"].(string)
	login := `{"username":"kris","password":"correct horse battery"}`
	if code, _ := do(t, h, "POST", "/api/auth/login", login, map[string]string{"X-Mailer": "1", "X-Device": second}); code != 403 {
		t.Errorf("unapproved device signed in: %d", code)
	}
	d, _ := s.Auth.Device(second)
	if code, _ := do(t, h, "POST", "/api/auth/devices/approve", `{"key":"`+d.Key+`","name":"Laptop"}`, admin); code != 200 {
		t.Fatalf("approve: %d", code)
	}
	code, out = do(t, h, "POST", "/api/auth/login", login, map[string]string{"X-Mailer": "1", "X-Device": second})
	if code != 200 {
		t.Fatalf("approved device: %d %v", code, out)
	}
	tok := out["token"].(string)

	// The token only works from the device it was issued to.
	if code, _ := do(t, h, "GET", "/api/overview", "", map[string]string{"X-Device": first, "Authorization": "Bearer " + tok}); code != 401 {
		t.Errorf("token from another device: %d", code)
	}
	if code, _ := do(t, h, "GET", "/api/overview", "", map[string]string{"X-Device": second, "Authorization": "Bearer " + tok}); code != 200 {
		t.Errorf("token from its device: %d", code)
	}

	// Removing the device ends its session at once.
	if code, _ := do(t, h, "POST", "/api/auth/devices/delete", `{"key":"`+d.Key+`"}`, admin); code != 200 {
		t.Fatalf("remove device: %d", code)
	}
	if code, _ := do(t, h, "GET", "/api/overview", "", map[string]string{"X-Device": second, "Authorization": "Bearer " + tok}); code != 401 {
		t.Errorf("removed device still signed in: %d", code)
	}

	// Five wrong passwords lock the name out, even with the right one.
	bad := `{"username":"kris","password":"wrong password here"}`
	for range 5 {
		do(t, h, "POST", "/api/auth/login", bad, map[string]string{"X-Mailer": "1", "X-Device": first})
	}
	if code, _ := do(t, h, "POST", "/api/auth/login", login, map[string]string{"X-Mailer": "1", "X-Device": first}); code != 429 {
		t.Errorf("after five failures: %d", code)
	}

	// A user account can't manage accounts or devices.
	code, out = do(t, h, "POST", "/api/auth/users", `{"username":"sam","role":"user"}`, admin)
	if code != 200 {
		t.Fatalf("add user: %d %v", code, out)
	}
	userTok, err := s.Auth.Login("sam", out["password"].(string), first, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	user := map[string]string{"X-Device": first, "Authorization": "Bearer " + userTok.Access}
	if code, _ := do(t, h, "GET", "/api/auth/devices", "", user); code != 403 {
		t.Errorf("user listed devices: %d", code)
	}
	if code, _ := do(t, h, "GET", "/api/overview", "", user); code != 200 {
		t.Errorf("user overview: %d", code)
	}
}

func TestRemoteHost(t *testing.T) {
	s := newServer(t)
	h := s.Handler()
	get := func(host string, hdr map[string]string) int {
		req := httptest.NewRequest("GET", "http://"+host+"/api/auth/status", nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}
	cf := map[string]string{"Cf-Connecting-Ip": "203.0.113.9"}
	if code := get("mail-admin.example.com", cf); code != 403 {
		t.Errorf("remote access off: %d", code)
	}
	s.Cfg.Update(func(c *config.Config) { c.Remote.Enabled = true })
	if code := get("mail-admin.example.com", cf); code != 200 {
		t.Errorf("remote access on: %d", code)
	}
	if code := get("other.example.com", cf); code != 403 {
		t.Errorf("another name: %d", code)
	}
}
