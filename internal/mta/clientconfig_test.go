package mta

import (
	"net/http/httptest"
	"strings"
	"testing"

	"mailer/internal/config"
)

func TestClientConfig(t *testing.T) {
	cfgm, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfgm.Update(func(c *config.Config) {
		c.Domains = []string{"example.com"}
		c.Tailscale.DNSName = "box.tailnet.ts.net"
	})
	h := (&MTA{Cfg: cfgm}).InboundHandler()
	get := func(method, host, path, body string) (int, string) {
		req := httptest.NewRequest(method, "https://"+host+path, strings.NewReader(body))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code, rr.Body.String()
	}

	code, body := get("POST", "autodiscover.example.com", "/Autodiscover/Autodiscover.xml",
		`<Autodiscover><Request><EMailAddress>kris@example.com</EMailAddress></Request></Autodiscover>`)
	for _, want := range []string{"<Type>IMAP</Type>", "<Server>box.tailnet.ts.net</Server>", "<Port>443</Port>",
		"<Port>8443</Port>", "<LoginName>kris@example.com</LoginName>", "<SSL>on</SSL>"} {
		if code != 200 || !strings.Contains(body, want) {
			t.Errorf("autodiscover missing %s: %d %s", want, code, body)
		}
	}
	if code, body := get("GET", "autodiscover.example.com", "/autodiscover/autodiscover.json/v1.0/kris@example.com?Protocol=AutodiscoverV1", ""); code != 200 ||
		!strings.Contains(body, "https://autodiscover.example.com/autodiscover/autodiscover.xml") {
		t.Errorf("autodiscover json: %d %s", code, body)
	}
	if code, body := get("GET", "autoconfig.example.com", "/mail/config-v1.1.xml?emailaddress=kris@example.com", ""); code != 200 ||
		!strings.Contains(body, "<hostname>box.tailnet.ts.net</hostname>") || !strings.Contains(body, "%EMAILADDRESS%") {
		t.Errorf("autoconfig: %d %s", code, body)
	}
	// Only our domains, and only the settings names.
	if code, _ := get("GET", "autoconfig.other.com", "/mail/config-v1.1.xml", ""); code != 404 {
		t.Errorf("other domain: %d", code)
	}
	if code, _ := get("GET", "mail-in.example.com", "/mail/config-v1.1.xml", ""); code != 404 {
		t.Errorf("inbound host: %d", code)
	}
}
