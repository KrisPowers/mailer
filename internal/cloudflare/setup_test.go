package cloudflare

import "testing"

func TestMergeSPF(t *testing.T) {
	cases := []struct{ existing, oldIP, want string }{
		{"", "", "v=spf1 include:_spf.mx.cloudflare.net ~all"},
		{"v=spf1 include:_spf.mx.cloudflare.net ~all", "", "v=spf1 include:_spf.mx.cloudflare.net ~all"},
		{"v=spf1 include:_spf.mx.cloudflare.net ip4:203.0.113.9 -all", "203.0.113.9", "v=spf1 include:_spf.mx.cloudflare.net -all"},
		{"v=spf1 include:_spf.google.com ~all", "", "v=spf1 include:_spf.google.com include:_spf.mx.cloudflare.net ~all"},
		{"v=spf1 ip4:198.51.100.8 include:_spf.mx.cloudflare.net ip4:192.0.2.4 ~all", "192.0.2.4", "v=spf1 ip4:198.51.100.8 include:_spf.mx.cloudflare.net ~all"},
		{"v=spf1 include:example.net redirect=_spf.example.net", "", "v=spf1 include:example.net include:_spf.mx.cloudflare.net redirect=_spf.example.net"},
	}
	for _, c := range cases {
		if got := MergeSPF(c.existing, c.oldIP); got != c.want {
			t.Errorf("MergeSPF(%q) = %q, want %q", c.existing, got, c.want)
		}
	}
}
