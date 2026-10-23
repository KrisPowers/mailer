package cloudflare

import "testing"

func TestMergeSPF(t *testing.T) {
	cases := []struct{ existing, newIP, oldIP, want string }{
		{"", "1.2.3.4", "", "v=spf1 include:_spf.mx.cloudflare.net ip4:1.2.3.4 ~all"},
		{"v=spf1 include:_spf.mx.cloudflare.net ~all", "1.2.3.4", "", "v=spf1 include:_spf.mx.cloudflare.net ip4:1.2.3.4 ~all"},
		{"v=spf1 include:_spf.mx.cloudflare.net ip4:9.9.9.9 -all", "1.2.3.4", "9.9.9.9", "v=spf1 include:_spf.mx.cloudflare.net ip4:1.2.3.4 -all"},
		{"v=spf1 include:_spf.google.com ~all", "1.2.3.4", "", "v=spf1 include:_spf.google.com include:_spf.mx.cloudflare.net ip4:1.2.3.4 ~all"},
		{"v=spf1 include:_spf.mx.cloudflare.net ip4:1.2.3.4 ~all", "1.2.3.4", "1.2.3.4", "v=spf1 include:_spf.mx.cloudflare.net ip4:1.2.3.4 ~all"},
	}
	for _, c := range cases {
		if got := MergeSPF(c.existing, c.newIP, c.oldIP); got != c.want {
			t.Errorf("MergeSPF(%q) = %q, want %q", c.existing, got, c.want)
		}
	}
}
