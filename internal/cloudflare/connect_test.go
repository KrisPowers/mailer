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
	"slices"
	"strings"
	"sync"
	"testing"

	"mailer/internal/config"
)

// fakeCF implements the slice of the Cloudflare API that Setup uses, for
// two zones in one account.
type fakeCF struct {
	mu       sync.Mutex
	zones    map[string]string            // zone ID -> name
	records  map[string]map[string]Record // zone ID -> record ID -> record
	nextID   int
	tunnels  int
	routing  map[string]bool
	catchAll map[string]string // zone ID -> worker, "" when off
	worker   map[string]any
	script   string
	tunnel   map[string]any             // tunnel configuration
	sending  map[string][]SendingDomain // zone ID -> onboarded domains
	sent     []map[string]any           // send_raw request bodies
}

func newFake() *fakeCF {
	f := &fakeCF{zones: map[string]string{"z1": "example.com", "z2": "example.org"},
		records: map[string]map[string]Record{}, routing: map[string]bool{}, catchAll: map[string]string{},
		sending: map[string][]SendingDomain{}}
	for id := range f.zones {
		f.records[id] = map[string]Record{}
	}
	return f
}

func (f *fakeCF) add(zone string, r Record) {
	f.nextID++
	r.ID = fmt.Sprint(f.nextID)
	f.records[zone][r.ID] = r
}

func (f *fakeCF) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ok := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, `{"success":true,"errors":[],"result":%s}`, b)
	}
	p := r.URL.Path
	zone, rest := "", ""
	if after, found := strings.CutPrefix(p, "/zones/"); found {
		zone, rest, _ = strings.Cut(after, "/")
		if _, known := f.zones[zone]; !known {
			http.Error(w, "unknown zone "+zone, 404)
			return
		}
	}
	if auth := r.Header.Get("Authorization"); !strings.HasPrefix(p, "/oauth2/") &&
		auth != "Bearer good-token" && auth != "Bearer access-1" && auth != "Bearer access-2" {
		fmt.Fprint(w, `{"success":false,"errors":[{"message":"Invalid API Token"}]}`)
		return
	}
	switch {
	case p == "/oauth2/token":
		r.ParseForm()
		switch {
		case r.Form.Get("client_id") != oauthClientID:
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_client"}`)
		case r.Form.Get("grant_type") == "authorization_code" && r.Form.Get("code") == "code-1" &&
			r.Form.Get("code_verifier") != "" && r.Form.Get("redirect_uri") == oauthRedirect:
			fmt.Fprint(w, `{"access_token":"access-1","refresh_token":"refresh-1","expires_in":3600}`)
		case r.Form.Get("grant_type") == "refresh_token" && r.Form.Get("refresh_token") == "refresh-1":
			fmt.Fprint(w, `{"access_token":"access-2","refresh_token":"refresh-2","expires_in":3600}`)
		default:
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_grant","error_description":"bad grant"}`)
		}
	case p == "/zones":
		var out []map[string]any
		for _, id := range []string{"z1", "z2"} {
			out = append(out, map[string]any{"id": id, "name": f.zones[id], "account": map[string]string{"id": "a1"}})
		}
		ok(out)
	case p == "/accounts/a1/cfd_tunnel" && r.Method == "POST":
		f.tunnels++
		ok(map[string]any{"id": "t1", "name": "mail"})
	case p == "/accounts/a1/cfd_tunnel/t1" && r.Method == "GET":
		ok(map[string]any{"id": "t1", "status": "healthy", "connections": []any{map[string]string{}}})
	case p == "/accounts/a1/cfd_tunnel/t1/token":
		ok("tunnel-token")
	case p == "/accounts/a1/cfd_tunnel/t1/configurations" && r.Method == "PUT":
		var body struct{ Config map[string]any }
		json.NewDecoder(r.Body).Decode(&body)
		f.tunnel = body.Config
		ok(map[string]any{})
	case p == "/accounts/a1/cfd_tunnel/t1/configurations":
		ok(map[string]any{"config": f.tunnel})
	case rest == "dns_records" && r.Method == "GET":
		out := []Record{}
		for _, rec := range f.records[zone] {
			if rec.Type == r.URL.Query().Get("type") && rec.Name == r.URL.Query().Get("name") {
				out = append(out, rec)
			}
		}
		ok(out)
	case rest == "dns_records" && r.Method == "POST":
		var rec Record
		json.NewDecoder(r.Body).Decode(&rec)
		f.add(zone, rec)
		ok(rec)
	case strings.HasPrefix(rest, "dns_records/") && r.Method == "PUT":
		var rec Record
		json.NewDecoder(r.Body).Decode(&rec)
		rec.ID = strings.TrimPrefix(rest, "dns_records/")
		f.records[zone][rec.ID] = rec
		ok(rec)
	case strings.HasPrefix(rest, "dns_records/") && r.Method == "DELETE":
		delete(f.records[zone], strings.TrimPrefix(rest, "dns_records/"))
		ok(map[string]any{})
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
	case rest == "email/routing" && r.Method == "GET":
		ok(map[string]any{"enabled": f.routing[zone], "status": map[bool]string{true: "ready", false: "unconfigured"}[f.routing[zone]]})
	case rest == "email/routing/dns":
		f.routing[zone] = true
		// Cloudflare publishes its MX and SPF records when enabling.
		name := f.zones[zone]
		for _, mx := range []string{"route1.mx.cloudflare.net", "route2.mx.cloudflare.net"} {
			f.add(zone, Record{Type: "MX", Name: name, Content: mx})
		}
		f.add(zone, Record{Type: "TXT", Name: name, Content: `"v=spf1 include:_spf.mx.cloudflare.net ~all"`})
		ok(map[string]any{})
	case rest == "email/routing/rules/catch_all" && r.Method == "PUT":
		var body struct {
			Enabled bool
			Actions []struct {
				Type  string
				Value []string
			} `json:"actions"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		f.catchAll[zone] = ""
		if body.Enabled && body.Actions[0].Type == "worker" {
			f.catchAll[zone] = body.Actions[0].Value[0]
		}
		ok(map[string]any{})
	case rest == "email/routing/rules/catch_all" && r.Method == "GET":
		target := f.catchAll[zone]
		ok(map[string]any{"enabled": target != "", "actions": []map[string]any{{"type": "worker", "value": []string{target}}}})
	case rest == "email/sending/subdomains" && r.Method == "GET":
		ok(append([]SendingDomain{}, f.sending[zone]...))
	case rest == "email/sending/subdomains" && r.Method == "POST":
		var body struct{ Name string }
		json.NewDecoder(r.Body).Decode(&body)
		sd := SendingDomain{Name: body.Name, Tag: "tag-" + zone, Enabled: true}
		f.sending[zone] = append(f.sending[zone], sd)
		ok(sd)
	case rest == "email/sending/subdomains/tag-"+zone+"/dns":
		name, ten := f.zones[zone], 10
		ok([]Record{
			{Type: "MX", Name: "cf-bounce." + name, Content: "route1.mx.cloudflare.net", Priority: &ten},
			{Type: "TXT", Name: "cf-bounce." + name, Content: `"v=spf1 include:_spf.mx.cloudflare.net ~all"`},
			{Type: "TXT", Name: "cf-bounce._domainkey." + name, Content: "v=DKIM1; k=rsa; p=CF"},
			{Type: "TXT", Name: "_dmarc." + name, Content: "v=DMARC1; p=reject"},
			{Type: "TXT", Name: name, Content: "v=spf1 include:_spf.mx.cloudflare.net -all"},
		})
	case p == "/accounts/a1/email/sending/send_raw":
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.sent = append(f.sent, body)
		ok(map[string]any{"delivered": []string{"ok@other.net"}, "permanent_bounces": []string{"BAD@other.net"}, "queued": []string{}})
	default:
		http.Error(w, "unexpected "+r.Method+" "+p, 404)
	}
}

func (f *fakeCF) find(zone, typ, name string) []Record {
	var out []Record
	for _, r := range f.records[zone] {
		if r.Type == typ && r.Name == name {
			out = append(out, r)
		}
	}
	return out
}

// Once one domain is set up, every other domain on the account is
// connected by itself.
func TestSyncZones(t *testing.T) {
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	apiBase = srv.URL

	cfgm, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Setup{Cfg: cfgm, Log: log.New(io.Discard, "", 0),
		DKIMRecord: func(d string) (string, error) { return "v=DKIM1; k=rsa; p=KEY-" + d, nil }}
	ctx := context.Background()
	s.UseToken("good-token")
	if err := s.SyncZones(ctx); err != nil || len(cfgm.Get().Domains) != 0 {
		t.Fatalf("synced before the first domain was set up: %v %v", err, cfgm.Get().Domains)
	}
	if _, err := s.Connect(ctx, "example.com"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := s.SyncZones(ctx); err != nil {
			t.Fatal(err)
		}
	}
	cfg := cfgm.Get()
	if !slices.Equal(cfg.Domains, []string{"example.com", "example.org"}) || cfg.Domain() != "example.com" {
		t.Errorf("domains after sync: %v", cfg.Domains)
	}
	if fake.catchAll["z2"] != workerName || len(fake.sending["z2"]) != 1 || fake.tunnels != 1 {
		t.Errorf("example.org not fully set up: catch-all %q, sending %v, tunnels %d", fake.catchAll["z2"], fake.sending["z2"], fake.tunnels)
	}
}

func TestConnect(t *testing.T) {
	fake := newFake()
	srv := httptest.NewServer(fake)
	defer srv.Close()
	apiBase = srv.URL

	cfgm, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Setup{Cfg: cfgm, Log: log.New(io.Discard, "", 0),
		DKIMRecord: func(d string) (string, error) { return "v=DKIM1; k=rsa; p=KEY-" + d, nil }}
	ctx := context.Background()

	if _, err := s.Connect(ctx, ""); err == nil {
		t.Fatal("connected without a credential")
	}
	s.UseToken("bad-token")
	if _, err := s.Connect(ctx, ""); err == nil {
		t.Fatal("bad token accepted")
	}
	s.UseToken(" good-token ")
	if _, err := s.Connect(ctx, ""); err == nil {
		t.Fatal("two zones visible but no choice asked for")
	}
	// The second run of each must be a no-op repair.
	for _, zone := range []string{"example.com", "example.com", "example.org", "example.org"} {
		if steps, err := s.Connect(ctx, zone); err != nil {
			t.Fatalf("%s: %v %+v", zone, err, steps)
		}
	}

	cfg := cfgm.Get()
	if !slices.Equal(cfg.Domains, []string{"example.com", "example.org"}) || cfg.Hostname != "mail.example.com" ||
		cfg.Cloudflare.TunnelToken != "tunnel-token" || cfg.Cloudflare.InboundHost != "mail-in.example.com" {
		t.Errorf("config not saved: %+v", cfg)
	}
	if fake.tunnels != 1 {
		t.Errorf("created %d tunnels, want 1 (shared, reused on repair)", fake.tunnels)
	}
	for _, z := range []string{"z1", "z2"} {
		if fake.catchAll[z] != workerName || !fake.routing[z] {
			t.Errorf("%s email routing: enabled=%v catch-all=%q", z, fake.routing[z], fake.catchAll[z])
		}
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

	check := func(zone, typ, name, content string) {
		t.Helper()
		recs := fake.find(zone, typ, name)
		if len(recs) != 1 {
			t.Errorf("%s %s: %d records, want 1", typ, name, len(recs))
			return
		}
		if unquote(recs[0].Content) != content {
			t.Errorf("%s %s = %q, want %q", typ, name, recs[0].Content, content)
		}
	}
	check("z1", "CNAME", "mail-in.example.com", "t1.cfargotunnel.com")
	if len(fake.find("z1", "A", "mail.example.com")) != 0 {
		t.Error("this machine's IP was published")
	}
	ingress, _ := json.Marshal(fake.tunnel)
	for _, h := range []string{"mail-in.example.com", "autodiscover.example.com", "autoconfig.example.com",
		"autodiscover.example.org", "autoconfig.example.org", "http_status:404"} {
		if !strings.Contains(string(ingress), h) {
			t.Errorf("tunnel ingress missing %s: %s", h, ingress)
		}
	}
	for z, d := range map[string]string{"z1": "example.com", "z2": "example.org"} {
		check(z, "CNAME", "autodiscover."+d, "t1.cfargotunnel.com")
		check(z, "CNAME", "autoconfig."+d, "t1.cfargotunnel.com")
		check(z, "TXT", "mail._domainkey."+d, "v=DKIM1; k=rsa; p=KEY-"+d)
		check(z, "TXT", "_dmarc."+d, defaultDMARC)
		check(z, "TXT", d, "v=spf1 include:_spf.mx.cloudflare.net ~all")
		check(z, "TXT", "cf-bounce._domainkey."+d, "v=DKIM1; k=rsa; p=CF")
		if len(fake.sending[z]) != 1 {
			t.Errorf("%s sending domains: %+v", d, fake.sending[z])
		}
	}
	if len(fake.find("z2", "CNAME", "mail-in.example.org")) != 0 {
		t.Error("secondary domain got its own tunnel")
	}
	if c := fake.find("z1", "CNAME", "mail-in.example.com"); len(c) == 1 && (c[0].Proxied == nil || !*c[0].Proxied) {
		t.Error("tunnel CNAME must be proxied")
	}

	st := s.CheckStatus(ctx, func(context.Context, string) error { return nil })
	if !st.Connected || st.TunnelStatus != "healthy" || len(st.Domains) != 2 {
		t.Fatalf("status: %+v", st)
	}
	for _, d := range st.Domains {
		if !d.OK() || len(d.Checks) != 8 {
			t.Errorf("%s not compliant: %+v", d.Name, d.Checks)
		}
	}

	// The home IP an older version published is taken out of SPF and DNS.
	for id, r := range fake.records["z1"] {
		if r.Type == "TXT" && r.Name == "example.com" {
			r.Content = "v=spf1 include:_spf.mx.cloudflare.net ip4:198.51.100.77 ~all"
			fake.records["z1"][id] = r
		}
	}
	fake.add("z1", Record{Type: "A", Name: "mail.example.com", Content: "198.51.100.77"})
	cfgm.Update(func(c *config.Config) {
		z := c.Cloudflare.Zones["example.com"]
		z.SPFIP = "198.51.100.77"
		c.Cloudflare.Zones["example.com"] = z
	})
	if err := s.SyncDNS(ctx); err != nil {
		t.Fatal(err)
	}
	check("z1", "TXT", "example.com", "v=spf1 include:_spf.mx.cloudflare.net ~all")
	if len(fake.find("z1", "A", "mail.example.com")) != 0 || cfgm.Get().Cloudflare.Zones["example.com"].SPFIP != "" {
		t.Error("old home IP not removed")
	}

	// A drifted record shows up as a failed check.
	for id, r := range fake.records["z2"] {
		if r.Name == "mail._domainkey.example.org" {
			r.Content = "v=DKIM1; k=rsa; p=OTHER"
			fake.records["z2"][id] = r
		}
	}
	st = s.CheckStatus(ctx, func(context.Context, string) error { return nil })
	if !st.Domains[0].OK() || st.Domains[1].OK() {
		t.Errorf("DKIM drift not caught: %+v", st.Domains)
	}

	if _, err := s.Disconnect(ctx, "example.com"); err == nil {
		t.Error("primary domain removed")
	}
	cfgm.Update(func(c *config.Config) { c.Aliases["hi@example.org"] = "x@example.com" })
	if warning, err := s.Disconnect(ctx, "example.org"); err != nil || warning != "" {
		t.Fatalf("disconnect: %q %v", warning, err)
	}
	cfg = cfgm.Get()
	if !slices.Equal(cfg.Domains, []string{"example.com"}) || len(cfg.Cloudflare.Zones) != 1 || len(cfg.Aliases) != 0 {
		t.Errorf("after disconnect: %+v", cfg)
	}
	if fake.catchAll["z2"] != "" || fake.catchAll["z1"] != workerName {
		t.Errorf("catch-all after disconnect: %v", fake.catchAll)
	}
}
