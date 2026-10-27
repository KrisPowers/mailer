package cloudflare

import "testing"

func TestMergeSPF(t *testing.T) {
	cases := []struct{ existing, oldIP, want string }{
		{"", "", "v=spf1 include:_spf.mx.cloudflare.net ~all"},
		{"v=spf1 include:_spf.mx.cloudflare.net ~all", "", "v=spf1 include:_spf.mx.cloudflare.net ~all"},
		{"v=spf1 include:_spf.mx.cloudflare.net ip4:9.9.9.9 -all", "9.9.9.9", "v=spf1 include:_spf.mx.cloudflare.net -all"},
		{"v=spf1 include:_spf.google.com ~all", "", "v=spf1 include:_spf.google.com include:_spf.mx.cloudflare.net ~all"},
		{"v=spf1 ip4:5.6.7.8 include:_spf.mx.cloudflare.net ip4:1.2.3.4 ~all", "1.2.3.4", "v=spf1 ip4:5.6.7.8 include:_spf.mx.cloudflare.net ~all"},
		{"v=spf1 include:other.net redirect=_spf.other.net", "", "v=spf1 include:other.net include:_spf.mx.cloudflare.net redirect=_spf.other.net"},
	}
	for _, c := range cases {
		if got := MergeSPF(c.existing, c.oldIP); got != c.want {
			t.Errorf("MergeSPF(%q) = %q, want %q", c.existing, got, c.want)
		}
	}
}
