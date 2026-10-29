package mta_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"mailer/internal/audit"
	"mailer/internal/config"
	"mailer/internal/imap"
	"mailer/internal/mailauth"
	"mailer/internal/maillog"
	"mailer/internal/mta"
	"mailer/internal/queue"
	"mailer/internal/smtp"
	"mailer/internal/smtpclient"
	"mailer/internal/store"
	"mailer/internal/users"
)

type fakeDNS struct{ txt map[string][]string }

func nf(n string) error { return &net.DNSError{Err: "no such host", Name: n, IsNotFound: true} }

func (f *fakeDNS) LookupTXT(_ context.Context, n string) ([]string, error) {
	if v, ok := f.txt[n]; ok {
		return v, nil
	}
	return nil, nf(n)
}
func (f *fakeDNS) LookupMX(_ context.Context, n string) ([]*net.MX, error) {
	if n == "other.net" {
		return []*net.MX{{Host: "mx.other.net.", Pref: 10}}, nil
	}
	return nil, nf(n)
}
func (f *fakeDNS) LookupIPAddr(_ context.Context, h string) ([]net.IPAddr, error) {
	if strings.TrimSuffix(h, ".") == "mx.other.net" {
		return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
	}
	return nil, nf(h)
}
func (f *fakeDNS) LookupAddr(context.Context, string) ([]string, error) { return nil, nf("") }

// recorder is a remote MX that captures what we send it.
type recorder struct {
	mu   sync.Mutex
	msgs []*smtp.Envelope
}

func (r *recorder) CheckSender(*smtp.Envelope) error            { return nil }
func (r *recorder) CheckRecipient(*smtp.Envelope, string) error { return nil }
func (r *recorder) Deliver(e *smtp.Envelope) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, e)
	return nil
}

func listen(t *testing.T, serve func(net.Listener, bool) error, terminated bool) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go serve(l, terminated)
	return l.Addr().String()
}

// client is a line-oriented test client for SMTP and IMAP.
type client struct {
	t *testing.T
	c net.Conn
	r *bufio.Reader
}

// dial connects the way Tailscale Funnel does: PROXY header first.
func dial(t *testing.T, addr string, proxy bool) *client {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.SetDeadline(time.Now().Add(30 * time.Second))
	if proxy {
		fmt.Fprintf(c, "PROXY TCP4 203.0.113.9 127.0.0.1 5555 443\r\n")
	}
	return &client{t: t, c: c, r: bufio.NewReader(c)}
}

func (c *client) send(s string) { fmt.Fprintf(c.c, "%s\r\n", s) }

func (c *client) line() string {
	l, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	return strings.TrimRight(l, "\r\n")
}

func (c *client) smtpCmd(cmd string, want string) string {
	if cmd != "" {
		c.send(cmd)
	}
	for {
		l := c.line()
		if len(l) >= 4 && l[3] == '-' {
			continue
		}
		if !strings.HasPrefix(l, want) {
			c.t.Fatalf("%q: got %q, want %s", cmd, l, want)
		}
		return l
	}
}

func (c *client) imapCmd(tag, cmd string) string {
	c.send(tag + " " + cmd)
	var all strings.Builder
	for {
		l := c.line()
		all.WriteString(l + "\n")
		if strings.HasSuffix(l, "}") {
			if i := strings.LastIndexByte(l, '{'); i >= 0 {
				var n int
				fmt.Sscanf(l[i+1:], "%d", &n)
				buf := make([]byte, n)
				io.ReadFull(c.r, buf)
				all.Write(buf)
				continue
			}
		}
		if strings.HasPrefix(l, tag+" ") {
			if !strings.HasPrefix(l, tag+" OK") {
				c.t.Fatalf("%s %s: %s\n%s", tag, cmd, l, all.String())
			}
			return all.String()
		}
	}
}

func TestEndToEnd(t *testing.T) {
	dir := t.TempDir()
	logger := log.New(io.Discard, "", 0)
	if testing.Verbose() {
		logger = log.Default()
	}
	cfgm, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfgm.Update(func(c *config.Config) {
		c.Domains = []string{"example.com"}
		c.Hostname = "mail.example.com"
		c.Aliases = map[string]string{"hello@example.com": "me@example.com"}
	})
	secret := cfgm.Get().Cloudflare.InboundSecret
	hostname := func() string { return cfgm.Get().Hostname }

	db, _ := users.Open(filepath.Join(dir, "users.json"))
	if err := db.Add("me@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	signer := &mailauth.Signer{Domain: "example.com", Selector: "mail", Key: key}
	dns := &fakeDNS{txt: map[string][]string{
		"mail._domainkey.example.com": {signer.DNSRecord()},
		"sender.test":                 {"v=spf1 ip4:198.51.100.0/24 -all"},
	}}

	st := store.New(filepath.Join(dir, "mail"))
	lim := users.NewLimiter()
	mailLog := maillog.Open(filepath.Join(dir, "mail.log"))
	m := &mta.MTA{Cfg: cfgm, Users: db, Store: st, Resolver: dns, Log: logger, Mail: mailLog,
		DKIM: func(d string) *mailauth.Signer {
			if d == "example.com" {
				return signer
			}
			return nil
		}}

	// Fake remote MX.
	rec := &recorder{}
	remote := &smtp.Server{Hostname: func() string { return "mx.other.net" }, Mode: smtp.ModeMX,
		Users: db, Limiter: lim, MaxSize: 10 << 20, Backend: rec, Log: logger}
	remoteAddr := listen(t, remote.Serve, false)

	sc := &smtpclient.Client{Hostname: hostname, Resolver: dns,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, remoteAddr)
		}}
	q, err := queue.New(filepath.Join(dir, "queue"), sc, m.Bounce, logger)
	if err != nil {
		t.Fatal(err)
	}
	q.Mail = mailLog
	m.Queue = q
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go q.Run(ctx)

	inbound := httptest.NewServer(m.InboundHandler())
	defer inbound.Close()
	signIns := audit.Open(filepath.Join(dir, "signins.log"))
	sub := &smtp.Server{Hostname: hostname, Mode: smtp.ModeSubmission, Users: db, Limiter: lim,
		MaxSize: 10 << 20, Backend: mta.Submission{MTA: m}, Log: logger, Audit: signIns}
	im := &imap.Server{Hostname: hostname, Users: db, Store: st, Limiter: lim, MaxMessage: 10 << 20, Log: logger,
		Audit: signIns}
	subAddr, imapAddr := listen(t, sub.Serve, true), listen(t, im.Serve, true)

	// post mimics the Cloudflare Email Worker.
	post := func(rcpt, secret, body string) int {
		req, _ := http.NewRequest("POST", inbound.URL+"/inbound", bytes.NewReader([]byte(body)))
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("X-Mail-From", "alice@sender.test")
		req.Header.Set("X-Rcpt-To", rcpt)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	msg := func(subject string) string {
		// Cloudflare's own headers come first.
		return "Received-SPF: pass (mx.cloudflare.net: domain of alice@sender.test designates 198.51.100.7 as permitted sender) client-ip=198.51.100.7; helo=mail.sender.test;\n" +
			"From: Alice <alice@sender.test>\nTo: hello@example.com\nSubject: " + subject +
			"\nMessage-ID: <1@sender.test>\nContent-Type: multipart/alternative; boundary=b1\n\n" +
			"--b1\nContent-Type: text/plain; charset=utf-8\n\nHi there\n.leading dot\n" +
			"--b1\nContent-Type: text/html\n\n<p>Hi there</p>\n--b1--\n"
	}
	if code := post("hello@example.com", "wrong", msg("x")); code != 401 {
		t.Errorf("bad secret: got %d", code)
	}
	if code := post("nobody@example.com", secret, msg("x")); code != 550 {
		t.Errorf("unknown recipient: got %d, want 550", code)
	}
	if code := post("hello@example.com", secret, msg("First message")); code != 200 {
		t.Fatalf("inbound: got %d", code)
	}

	// --- IMAP over a Funnel-style connection ---
	ic := dial(t, imapAddr, true)
	if l := ic.line(); !strings.HasPrefix(l, "* OK") || !strings.Contains(l, "AUTH=PLAIN") {
		t.Fatalf("greeting %q", l)
	}
	auth := base64.StdEncoding.EncodeToString([]byte("\x00me@example.com\x00correct horse battery"))
	ic.imapCmd("a1", "AUTHENTICATE PLAIN "+auth)

	list := ic.imapCmd("a2", `LIST "" "*"`)
	for _, want := range []string{`\Sent) "/" "Sent"`, `\Junk) "/" "Junk"`, `"INBOX"`} {
		if !strings.Contains(list, want) {
			t.Errorf("LIST missing %s:\n%s", want, list)
		}
	}
	sel := ic.imapCmd("a3", "SELECT INBOX")
	if !strings.Contains(sel, "* 1 EXISTS") || !strings.Contains(sel, "[UNSEEN 1]") {
		t.Fatalf("SELECT:\n%s", sel)
	}
	f := ic.imapCmd("a4", "FETCH 1 (UID FLAGS ENVELOPE BODYSTRUCTURE BODY.PEEK[HEADER.FIELDS (Subject)] BODY.PEEK[1])")
	for _, want := range []string{
		`"First message"`, `(("Alice" NIL "alice" "sender.test"))`,
		`"ALTERNATIVE"`, `"TEXT" "PLAIN" ("CHARSET" "utf-8")`,
		`BODY[HEADER.FIELDS ("SUBJECT")]`, "Subject: First message",
		"Hi there\r\n.leading dot", "FLAGS ()",
	} {
		if !strings.Contains(f, want) {
			t.Errorf("FETCH missing %q:\n%s", want, f)
		}
	}
	full := ic.imapCmd("a5", "FETCH 1 BODY[]")
	for _, want := range []string{"spf=pass smtp.mailfrom=sender.test", "[198.51.100.7]", "via Cloudflare Email Routing", `\Seen`} {
		if !strings.Contains(full, want) {
			t.Errorf("BODY[] missing %q:\n%s", want, full)
		}
	}
	if s := ic.imapCmd("a7", `SEARCH FROM "alice" SUBJECT first`); !strings.Contains(s, "* SEARCH 1") {
		t.Errorf("search: %s", s)
	}

	draft := "From: me@example.com\r\nSubject: draft\r\n\r\nwip\r\n"
	ic.send(fmt.Sprintf("a8 APPEND Drafts (\\Draft) {%d}", len(draft)))
	if l := ic.line(); !strings.HasPrefix(l, "+") {
		t.Fatalf("expected continuation, got %q", l)
	}
	ic.c.Write([]byte(draft + "\r\n"))
	if l := ic.line(); !strings.Contains(l, "a8 OK [APPENDUID") {
		t.Fatalf("APPEND: %q", l)
	}
	mv := ic.imapCmd("b1", "UID MOVE 1 Archive")
	if !strings.Contains(mv, "COPYUID") || !strings.Contains(mv, "* 1 EXPUNGE") {
		t.Errorf("MOVE:\n%s", mv)
	}

	// IDLE sees new mail.
	ic.send("b2 IDLE")
	if l := ic.line(); !strings.HasPrefix(l, "+") {
		t.Fatalf("IDLE: %q", l)
	}
	post("hello@example.com", secret, msg("Second message"))
	if l := ic.line(); l != "* 1 EXISTS" {
		t.Fatalf("IDLE expected EXISTS, got %q", l)
	}
	ic.send("DONE")
	if l := ic.line(); !strings.HasPrefix(l, "b2 OK") {
		t.Fatalf("IDLE end: %q", l)
	}

	// --- Submission over a Funnel-style connection ---
	sc2 := dial(t, subAddr, true)
	sc2.smtpCmd("", "220")
	if ehlo := sc2.smtpCmd("EHLO laptop", "250"); false {
		_ = ehlo
	}
	sc2.smtpCmd("MAIL FROM:<me@example.com>", "530")
	sc2.smtpCmd("AUTH PLAIN "+auth, "235")
	sc2.smtpCmd("MAIL FROM:<ceo@bigcorp.com>", "553")
	sc2.smtpCmd("MAIL FROM:<hello@example.com>", "250")
	sc2.smtpCmd("RCPT TO:<bob@other.net>", "250")
	sc2.smtpCmd("DATA", "354")
	sc2.send("From: Me <hello@example.com>\r\nTo: bob@other.net\r\nSubject: Re: hi\r\n\r\nReply body\r\n.")
	sc2.smtpCmd("", "250")
	sc2.smtpCmd("QUIT", "221")

	deadline := time.Now().Add(10 * time.Second)
	for {
		rec.mu.Lock()
		n := len(rec.msgs)
		rec.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("outbound message never reached remote MX")
		}
		time.Sleep(50 * time.Millisecond)
	}
	out := rec.msgs[0]
	if out.From != "hello@example.com" || out.To[0] != "bob@other.net" {
		t.Errorf("envelope %+v", out)
	}
	// A receive lock refuses mail for the account and its aliases.
	db.SetLocks("me@example.com", users.Locks{Receive: true})
	if code := post("hello@example.com", secret, msg("locked")); code != 550 {
		t.Errorf("receive lock: got %d, want 550", code)
	}
	db.SetLocks("me@example.com", users.Locks{})

	// The mail log has the received messages, the refusal and the send.
	var log []string
	for _, e := range mailLog.Recent() {
		log = append(log, e.Dir+" "+e.Status+" "+e.To)
	}
	for _, want := range []string{"in rejected nobody@example.com", "in delivered hello@example.com", "out sent bob@other.net"} {
		if !slices.Contains(log, want) {
			t.Errorf("mail log missing %q: %q", want, log)
		}
	}

	res := mailauth.VerifyDKIM(context.Background(), dns, out.Data)
	if len(res) != 1 || res[0].Status != mailauth.DKIMPass {
		t.Errorf("outbound DKIM: %+v", res)
	}

	// Wrong passwords and unknown accounts are refused.
	wc := dial(t, imapAddr, true)
	wc.line()
	wc.send(`w1 ID ("name" "Microsoft Outlook" "version" "16.0")`)
	wc.line()
	wc.line()
	for _, l := range []string{"w2 LOGIN me@example.com wrong-password", "w3 LOGIN nobody@example.com wrong-password"} {
		wc.send(l)
		if r := wc.line(); !strings.Contains(r, "AUTHENTICATIONFAILED") {
			t.Errorf("%s: %q", l, r)
		}
	}

	// A literal length that would wrap the running total is refused, not
	// allocated.
	oc := dial(t, imapAddr, true)
	oc.line()
	oc.send("o1 LOGIN {1}")
	oc.line()
	oc.send("x {9223372036854775807}")
	if l := oc.line(); !strings.Contains(l, "BYE") {
		t.Errorf("huge literal: got %q", l)
	}

	// Failed logins from the real client IP (from the PROXY header) get
	// rate-limited without locking out other addresses.
	for i := 0; i < 5; i++ {
		lim.Fail("203.0.113.9")
	}
	bc := dial(t, imapAddr, true)
	bc.line()
	bc.send("z1 LOGIN me@example.com x")
	if l := bc.line(); !strings.Contains(l, "UNAVAILABLE") {
		t.Errorf("expected rate limit for 203.0.113.9, got %q", l)
	}
	if b := lim.BlockedIPs(); len(b) != 1 || b["203.0.113.9"].IsZero() {
		t.Errorf("blocked: %v", b)
	}

	// Every attempt is in the sign-in audit, newest first, with the real
	// client IP, the app's name and the commands it sent first.
	var got []string
	for _, e := range signIns.Recent() {
		got = append(got, e.Proto+" "+e.Result)
		if e.IP != "203.0.113.9" {
			t.Errorf("audit IP %q", e.IP)
		}
	}
	want := []string{"imap blocked", "imap unknown_user", "imap bad_password", "smtp ok", "imap ok"}
	if !slices.Equal(got, want) {
		t.Errorf("audit = %q, want %q", got, want)
	}
	if e := signIns.Recent(); len(e) == 5 {
		if e[1].Client != "Microsoft Outlook 16.0" || !slices.Equal(e[1].Commands, []string{"ID", "LOGIN", "LOGIN"}) {
			t.Errorf("unknown_user event: %+v", e[1])
		}
		if e[3].Client != "laptop" || !slices.Equal(e[3].Commands, []string{"EHLO", "MAIL", "AUTH PLAIN"}) {
			t.Errorf("smtp event: %+v", e[3])
		}
		if e[4].User != "me@example.com" || !slices.Equal(e[4].Commands, []string{"AUTHENTICATE PLAIN"}) {
			t.Errorf("imap event: %+v", e[4])
		}
	}
	lim.Unblock("203.0.113.9")
	if lim.Blocked("203.0.113.9") {
		t.Error("unblock didn't")
	}
}
