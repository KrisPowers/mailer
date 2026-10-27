package cloudflare

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"testing"

	"mailer/internal/config"
)

func TestSending(t *testing.T) {
	fake := newFake()
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
	ctx := context.Background()
	if _, err := s.Connect(ctx, "good-token", "example.com"); err != nil {
		t.Fatal(err)
	}

	// Onboarding twice publishes each record once.
	for range 2 {
		if err := s.EnableSending(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.sending["z1"]) != 1 {
		t.Errorf("sending domains: %+v", fake.sending["z1"])
	}
	one := func(typ, name, content string) {
		t.Helper()
		recs := fake.find("z1", typ, name)
		if len(recs) != 1 || unquote(recs[0].Content) != content {
			t.Errorf("%s %s = %+v, want one %q", typ, name, recs, content)
		}
	}
	one("MX", "cf-bounce.example.com", "route1.mx.cloudflare.net")
	one("TXT", "cf-bounce.example.com", "v=spf1 include:_spf.mx.cloudflare.net ~all")
	one("TXT", "cf-bounce._domainkey.example.com", "v=DKIM1; k=rsa; p=CF")
	// The domain's own DMARC and SPF are kept.
	one("TXT", "_dmarc.example.com", defaultDMARC)
	one("TXT", "example.com", "v=spf1 include:_spf.mx.cloudflare.net ip4:203.0.113.50 ~all")
	if mx := fake.find("z1", "MX", "cf-bounce.example.com"); len(mx) == 1 && (mx[0].Priority == nil || *mx[0].Priority != 10) {
		t.Error("MX priority not kept")
	}

	cfgm.Update(func(c *config.Config) { c.SendViaCloudflare = true })
	st := s.CheckStatus(ctx, func(context.Context, string) error { return nil })
	if d := st.Domains[0]; !d.OK() || len(d.Checks) != 7 {
		t.Errorf("status with sending on: %+v", d.Checks)
	}

	relay := &Relay{Cfg: cfgm}
	msg := []byte("From: me@example.com\r\nTo: ok@other.net\r\nSubject: hi\r\n\r\nhello\r\n")
	res := relay.Deliver(ctx, "other.net", "me@example.com", []string{"ok@other.net", "bad@other.net"}, msg)
	if len(fake.sent) != 1 || fake.sent[0]["mime_message"] != string(msg) || fake.sent[0]["from"] != "me@example.com" {
		t.Fatalf("send_raw request: %+v", fake.sent)
	}
	if res[0].Err != nil || res[1].Err == nil || !res[1].Permanent {
		t.Errorf("results: %+v", res)
	}
}
