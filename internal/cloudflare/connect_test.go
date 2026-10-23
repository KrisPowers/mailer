package cloudflare

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"mailer/internal/config"
)

// fakeCF implements the slice of the Cloudflare API that Setup uses.
type fakeCF struct {
	mu       sync.Mutex
	records  map[string]Record
	nextID   int
	tunnels  int
	routing  bool
	catchAll string
	worker   map[string]any
	script   string
}

func (f *fakeCF) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, `{"success":true,"errors":[],"result":%s}`, b)
	}
	p := r.URL.Path
	switch {
	case p == "/user/tokens/verify":
		if r.Header.Get("Authorization") != "Bearer good-token" {
			fmt.Fprint(w, `{"success":false,"errors":[{"message":"Invalid API Token"}]}`)
			return
		}
		ok(map[string]string{"status": "active"})
	case p == "/zones":
		ok([]map[string]any{{"id": "z1", "name": "example.com", "account": map[string]string{"id": "a1"}}})
	case p == "/accounts/a1/cfd_tunnel" && r.Method == "POST":
		f.tunnels++
		ok(map[string]any{"id": "t1", "name": "mailer"})
	case p == "/accounts/a1/cfd_tunnel/t1" && r.Method == "GET":
		ok(map[string]any{"id": "t1", "status": "healthy", "connections": []any{map[string]string{}}})
	case p == "/accounts/a1/cfd_tunnel/t1/token":
		ok("tunnel-token")
	case p == "/accounts/a1/cfd_tunnel/t1/configurations":
		ok(map[string]any{})
	case p == "/zones/z1/dns_records" && r.Method == "GET":
		var out []Record
		for _, rec := range f.records {
			if rec.Type == r.URL.Query().Get("type") && rec.Name == r.URL.Query().Get("name") {
				out = append(out, rec)
			}
		}
		ok(out)
	case p == "/zones/z1/dns_records" && r.Method == "POST":
		var rec Record
		json.NewDecoder(r.Body).Decode(&rec)
		f.nextID++
		rec.ID = fmt.Sprint(f.nextID)
		f.records[rec.ID] = rec
		ok(rec)
	case strings.HasPrefix(p, "/zones/z1/dns_records/") && r.Method == "PUT":
		var rec Record
		json.NewDecoder(r.Body).Decode(&rec)
		rec.ID = strings.TrimPrefix(p, "/zones/z1/dns_records/")
		f.records[rec.ID] = rec
		ok(rec)
	case strings.HasPrefix(p, "/accounts/a1/workers/scripts/"):
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			b, _ := io.ReadAll(part)
			if part.FormName() == "metadata" {
				json.Unmarshal(b, &f.worker)
			} else if part.Header.Get("Content-Type") == "application/javascript+module" {
				f.script = string(b)
			}
		}
		ok(map[string]any{})
	case p == "/zones/z1/email/routing" && r.Method == "GET":
		ok(map[string]any{"enabled": f.routing, "status": map[bool]string{true: "ready", false: "unconfigured"}[f.routing]})
	case p == "/zones/z1/email/routing/dns":
		f.routing = true
		// Cloudflare publishes its MX and SPF records when enabling.
		f.nextID++
		f.records[fmt.Sprint(f.nextID)] = Record{ID: fmt.Sprint(f.nextID), Type: "TXT", Name: "example.com",
			Content: `"v=spf1 include:_spf.mx.cloudflare.net ~all"`}
		ok(map[string]any{})
	case p == "/zones/z1/email/routing/rules/catch_all" && r.Method == "PUT":
		var body struct {
			Actions []struct{ Value []string } `json:"actions"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.catchAll = body.Actions[0].Value[0]
		ok(map[string]any{})
	default:
		http.Error(w, "unexpected "+r.Method+" "+p, 404)
	}
}

func (f *fakeCF) find(typ, name string) []Record {
	var out []Record
	for _, r := range f.records {
		if r.Type == typ && r.Name == name {
			out = append(out, r)
		}
	}
	return out
}

func TestConnect(t *testing.T) {
	fake := &fakeCF{records: map[string]Record{}}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	apiBase = srv.URL
	publicIP = func(context.Context) (string, error) { return "203.0.113.50", nil }

	cfgm, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Setup{Cfg: cfgm, Log: log.New(io.Discard, "", 0),
		DKIMRecord: func(d string) (string, error) { return "v=DKIM1; k=rsa; p=KEY", nil }}

	if _, err := s.Connect(context.Background(), "bad-token", ""); err == nil {
		t.Fatal("bad token accepted")
	}
	for i := 0; i < 2; i++ { // second run must be a no-op repair
		steps, err := s.Connect(context.Background(), "good-token", "")
		if err != nil {
			t.Fatalf("run %d: %v %+v", i, err, steps)
		}
	}

	cfg := cfgm.Get()
	if cfg.Domain() != "example.com" || cfg.Hostname != "mail.example.com" || cfg.Cloudflare.TunnelToken != "tunnel-token" {
		t.Errorf("config not saved: %+v", cfg)
	}
	if fake.tunnels != 1 {
		t.Errorf("created %d tunnels, want 1 (reuse on repair)", fake.tunnels)
	}
	if fake.catchAll != cfg.Cloudflare.WorkerName || !fake.routing {
		t.Errorf("email routing: enabled=%v catch-all=%q", fake.routing, fake.catchAll)
	}
	if !strings.Contains(fake.script, "async email(message, env") {
		t.Error("worker script not uploaded")
	}
	b, _ := json.Marshal(fake.worker)
	for _, want := range []string{`"main_module":"inbound.js"`, `https://mail-in.example.com/inbound`, cfg.Cloudflare.InboundSecret} {
		if !strings.Contains(string(b), want) {
			t.Errorf("worker metadata missing %s: %s", want, b)
		}
	}

	check := func(typ, name, content string) {
		t.Helper()
		recs := fake.find(typ, name)
		if len(recs) != 1 {
			t.Errorf("%s %s: %d records, want 1", typ, name, len(recs))
			return
		}
		if unquote(recs[0].Content) != content {
			t.Errorf("%s %s = %q, want %q", typ, name, recs[0].Content, content)
		}
	}
	check("CNAME", "mail-in.example.com", "t1.cfargotunnel.com")
	check("TXT", "mail._domainkey.example.com", "v=DKIM1; k=rsa; p=KEY")
	check("TXT", "_dmarc.example.com", "v=DMARC1; p=quarantine; adkim=r; aspf=r")
	check("TXT", "example.com", "v=spf1 include:_spf.mx.cloudflare.net ip4:203.0.113.50 ~all")
	check("A", "mail.example.com", "203.0.113.50")
	if a := fake.find("A", "mail.example.com"); len(a) == 1 && (a[0].Proxied == nil || *a[0].Proxied) {
		t.Error("A record must not be proxied")
	}
	if c := fake.find("CNAME", "mail-in.example.com"); len(c) == 1 && (c[0].Proxied == nil || !*c[0].Proxied) {
		t.Error("tunnel CNAME must be proxied")
	}

	// IP change: SPF and A follow, old IP removed.
	publicIP = func(context.Context) (string, error) { return "198.51.100.77", nil }
	if _, err := s.SyncDNS(context.Background()); err != nil {
		t.Fatal(err)
	}
	check("TXT", "example.com", "v=spf1 include:_spf.mx.cloudflare.net ip4:198.51.100.77 ~all")
	check("A", "mail.example.com", "198.51.100.77")
}
