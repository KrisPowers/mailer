package cloudflare

import (
	"bytes"
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

	cfgm, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := &Setup{Cfg: cfgm, Log: log.New(io.Discard, "", 0),
		DKIMRecord: func(d string) (string, error) { return "v=DKIM1; k=rsa; p=KEY", nil }}
	ctx := context.Background()
	s.UseToken("good-token")

	// Connecting twice onboards Email Sending once and publishes each
	// record once.
	for range 2 {
		if _, err := s.Connect(ctx, "example.com"); err != nil {
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
	one("TXT", "example.com", "v=spf1 include:_spf.mx.cloudflare.net ~all")
	if mx := fake.find("z1", "MX", "cf-bounce.example.com"); len(mx) == 1 && (mx[0].Priority == nil || *mx[0].Priority != 10) {
		t.Error("MX priority not kept")
	}

	st := s.CheckStatus(ctx, func(context.Context, string) error { return nil })
	if d := st.Domains[0]; !d.OK() || len(d.Checks) != 8 {
		t.Errorf("status: %+v", d.Checks)
	}

	relay := &Relay{Setup: s}
	msg := []byte("From: me@example.com\r\nTo: ok@example.net\r\nSubject: hi\r\n\r\nhello\r\n")
	res := relay.Deliver(ctx, "example.net", "me@example.com", []string{"ok@example.net", "bad@example.net"}, msg)
	if len(fake.sent) != 1 || fake.sent[0]["mime_message"] != string(msg) || fake.sent[0]["from"] != "me@example.com" {
		t.Fatalf("send_raw request: %+v", fake.sent)
	}
	if res[0].Err != nil || res[1].Err == nil || !res[1].Permanent {
		t.Errorf("results: %+v", res)
	}

	// Nothing goes out directly: what Email Sending can't take fails for good.
	for name, c := range map[string]struct {
		from string
		data []byte
	}{
		"too big":        {"me@example.com", append(bytes.Repeat([]byte("x"), MaxSendBytes), msg...)},
		"not UTF-8":      {"me@example.com", append([]byte("X-Bad: \xff\r\n"), msg...)},
		"unknown domain": {"me@elsewhere.example", msg},
	} {
		res := relay.Deliver(ctx, "example.net", c.from, []string{"ok@example.net"}, c.data)
		if res[0].Err == nil || !res[0].Permanent {
			t.Errorf("%s: %+v", name, res)
		}
	}
	if len(fake.sent) != 1 {
		t.Errorf("sent %d messages, want 1", len(fake.sent))
	}
}
