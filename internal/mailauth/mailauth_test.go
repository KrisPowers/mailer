package mailauth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"strings"
	"testing"
)

type fakeResolver struct {
	txt map[string][]string
	mx  map[string][]*net.MX
	ip  map[string][]string
}

func notFound(name string) error {
	return &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
}

func (f *fakeResolver) LookupTXT(_ context.Context, name string) ([]string, error) {
	if v, ok := f.txt[name]; ok {
		return v, nil
	}
	return nil, notFound(name)
}
func (f *fakeResolver) LookupMX(_ context.Context, name string) ([]*net.MX, error) {
	if v, ok := f.mx[name]; ok {
		return v, nil
	}
	return nil, notFound(name)
}
func (f *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	v, ok := f.ip[strings.TrimSuffix(host, ".")]
	if !ok {
		return nil, notFound(host)
	}
	var out []net.IPAddr
	for _, s := range v {
		out = append(out, net.IPAddr{IP: net.ParseIP(s)})
	}
	return out, nil
}
func (f *fakeResolver) LookupAddr(context.Context, string) ([]string, error) {
	return nil, notFound("")
}

func TestSPF(t *testing.T) {
	r := &fakeResolver{
		txt: map[string][]string{
			"example.com":      {"v=spf1 ip4:192.0.2.0/24 include:_spf.example.net mx -all"},
			"_spf.example.net": {"v=spf1 ip4:198.51.100.7 ~all"},
			"soft.example":     {"v=spf1 ~all"},
			"redir.example":    {"v=spf1 redirect=example.com"},
			"macro.example":    {"v=spf1 exists:%{i}._spf.macro.example -all"},
			"broken.example":   {"v=spf1 foo:bar -all"},
		},
		mx: map[string][]*net.MX{"example.com": {{Host: "mx.example.com.", Pref: 10}}},
		ip: map[string][]string{"mx.example.com": {"203.0.113.5"}, "203.0.113.9._spf.macro.example": {"127.0.0.2"}},
	}
	cases := []struct {
		ip, sender string
		want       SPFResult
	}{
		{"192.0.2.44", "a@example.com", SPFPass},
		{"198.51.100.7", "a@example.com", SPFPass},
		{"203.0.113.5", "a@example.com", SPFPass},
		{"10.0.0.1", "a@example.com", SPFFail},
		{"10.0.0.1", "a@soft.example", SPFSoftFail},
		{"192.0.2.1", "a@redir.example", SPFPass},
		{"203.0.113.9", "a@macro.example", SPFPass},
		{"203.0.113.8", "a@macro.example", SPFFail},
		{"10.0.0.1", "a@nothing.example", SPFNone},
		{"10.0.0.1", "a@broken.example", SPFPermError},
	}
	for _, c := range cases {
		got, _ := CheckSPF(context.Background(), r, net.ParseIP(c.ip), c.sender, "helo.test")
		if got != c.want {
			t.Errorf("SPF(%s, %s) = %s, want %s", c.ip, c.sender, got, c.want)
		}
	}
}

func TestDKIMRoundTripAndDMARC(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := &Signer{Domain: "example.com", Selector: "mail", Key: key}
	msg := []byte("From: Me <me@example.com>\r\nTo: you@example.net\r\nSubject:  Hello   there\r\n" +
		"Date: Mon, 1 Jan 2024 00:00:00 +0000\r\n\r\nBody line  one \r\n.dot line\r\n\r\n\r\n")
	sig, err := s.Sign(msg)
	if err != nil {
		t.Fatal(err)
	}
	signed := append(sig, msg...)
	r := &fakeResolver{txt: map[string][]string{
		"mail._domainkey.example.com": {s.DNSRecord()},
		"_dmarc.example.com":          {"v=DMARC1; p=reject"},
	}}

	res := VerifyDKIM(context.Background(), r, signed)
	if len(res) != 1 || res[0].Status != DKIMPass {
		t.Fatalf("verify: %+v", res)
	}

	// Relaxed canonicalization tolerates whitespace changes in headers.
	munged := strings.Replace(string(signed), "Subject:  Hello   there", "Subject: Hello there", 1)
	if res := VerifyDKIM(context.Background(), r, []byte(munged)); res[0].Status != DKIMPass {
		t.Fatalf("relaxed header verify: %+v", res)
	}

	// Content changes break it.
	tampered := strings.Replace(string(signed), "Body line", "Evil line", 1)
	res = VerifyDKIM(context.Background(), r, []byte(tampered))
	if res[0].Status != DKIMFail {
		t.Fatalf("tampered message verified: %+v", res)
	}

	d := CheckDMARC(context.Background(), r, "example.com", SPFFail, "example.com", VerifyDKIM(context.Background(), r, signed))
	if d.Result != "pass" {
		t.Errorf("DMARC with DKIM pass = %+v", d)
	}
	d = CheckDMARC(context.Background(), r, "example.com", SPFFail, "example.com", res)
	if d.Result != "fail" || d.Policy != "reject" {
		t.Errorf("DMARC with failures = %+v", d)
	}
	d = CheckDMARC(context.Background(), r, "sub.example.com", SPFPass, "bounce.example.com", nil)
	if d.Result != "pass" {
		t.Errorf("relaxed SPF alignment via org domain = %+v", d)
	}
}

func TestOrgDomain(t *testing.T) {
	for in, want := range map[string]string{
		"a.b.example.com": "example.com",
		"example.com":     "example.com",
		"mail.foo.co.uk":  "foo.co.uk",
	} {
		if got := OrgDomain(in); got != want {
			t.Errorf("OrgDomain(%s) = %s, want %s", in, got, want)
		}
	}
}
