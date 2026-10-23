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
	return &Server{Cfg: cfgm, Users: db, Store: st, Queue: q, MTA: m,
		Setup: &cloudflare.Setup{Cfg: cfgm, Log: logger}, Tunnel: &tunnel.Runner{Log: logger},
		Logs: logbuf.New(10), Log: logger}
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
}

func TestAccounts(t *testing.T) {
	s := newServer(t)
	h := s.Handler()
	hdr := map[string]string{"X-Mailer": "1"}

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
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	var list []accountInfo
	json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list) != 1 || len(list[0].Mailboxes) != 6 || len(list[0].Aliases) != 2 {
		t.Errorf("accounts list: %+v", list)
	}

	code, out = do(t, h, "POST", "/api/accounts/password", `{"email":"kris@example.com"}`, hdr)
	if code != 200 || out["password"] == pw {
		t.Errorf("reset: %d %v", code, out)
	}
	if code, _ := do(t, h, "POST", "/api/accounts/delete", `{"email":"kris@example.com"}`, hdr); code != 200 || s.Users.Exists("kris@example.com") {
		t.Error("delete failed")
	}
}
