package mta

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"mailer/internal/config"
)

var emailRe = regexp.MustCompile(`(?i)<EMailAddress>\s*([^<\s]+)\s*</EMailAddress>`)

// clientConfig answers the lookups mail apps make to find the server's
// settings from an address alone, reached through the Cloudflare Tunnel at
// autodiscover.<domain> and autoconfig.<domain>:
//
//	Autodiscover (Outlook)   /autodiscover/autodiscover.xml, and the .json
//	                         lookup that points newer Outlook at it
//	autoconfig (Thunderbird) /mail/config-v1.1.xml
func (m *MTA) clientConfig(w http.ResponseWriter, r *http.Request) {
	cfg := m.Cfg.Get()
	host, _, _ := strings.Cut(strings.ToLower(r.Host), ":")
	domain := strings.TrimPrefix(strings.TrimPrefix(host, "autodiscover."), "autoconfig.")
	server := cfg.Tailscale.DNSName
	if domain == host || !cfg.IsLocalDomain(domain) || server == "" {
		http.NotFound(w, r)
		return
	}
	path := strings.ToLower(r.URL.Path)
	switch {
	case strings.HasPrefix(path, "/autodiscover/autodiscover.json"):
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"Protocol": "AutodiscoverV1",
			"Url":      "https://" + host + "/autodiscover/autodiscover.xml",
		})
	case path == "/autodiscover/autodiscover.xml":
		body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		login := ""
		if mt := emailRe.FindSubmatch(body); mt != nil {
			login = fmt.Sprintf("\n        <LoginName>%s</LoginName>", esc(string(mt[1])))
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?>
<Autodiscover xmlns="http://schemas.microsoft.com/exchange/autodiscover/responseschema/2006">
  <Response xmlns="http://schemas.microsoft.com/exchange/autodiscover/outlook/responseschema/2006a">
    <Account>
      <AccountType>email</AccountType>
      <Action>settings</Action>
      <Protocol>
        <Type>IMAP</Type>
        <Server>%[1]s</Server>
        <Port>%[2]d</Port>%[4]s
        <DomainRequired>off</DomainRequired>
        <SPA>off</SPA>
        <SSL>on</SSL>
        <AuthRequired>on</AuthRequired>
      </Protocol>
      <Protocol>
        <Type>SMTP</Type>
        <Server>%[1]s</Server>
        <Port>%[3]d</Port>%[4]s
        <DomainRequired>off</DomainRequired>
        <SPA>off</SPA>
        <SSL>on</SSL>
        <AuthRequired>on</AuthRequired>
        <UsePOPAuth>off</UsePOPAuth>
        <SMTPLast>off</SMTPLast>
      </Protocol>
    </Account>
  </Response>
</Autodiscover>
`, esc(server), config.PublicIMAPPort, config.PublicSubmissionPort, login)
	case path == "/mail/config-v1.1.xml" || path == "/.well-known/autoconfig/mail/config-v1.1.xml":
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		fmt.Fprintf(w, `<?xml version="1.0" encoding="utf-8"?>
<clientConfig version="1.1">
  <emailProvider id="%[1]s">
    <domain>%[1]s</domain>
    <displayName>%[1]s</displayName>
    <incomingServer type="imap">
      <hostname>%[2]s</hostname>
      <port>%[3]d</port>
      <socketType>SSL</socketType>
      <username>%%EMAILADDRESS%%</username>
      <authentication>password-cleartext</authentication>
    </incomingServer>
    <outgoingServer type="smtp">
      <hostname>%[2]s</hostname>
      <port>%[4]d</port>
      <socketType>SSL</socketType>
      <username>%%EMAILADDRESS%%</username>
      <authentication>password-cleartext</authentication>
    </outgoingServer>
  </emailProvider>
</clientConfig>
`, esc(domain), esc(server), config.PublicIMAPPort, config.PublicSubmissionPort)
	default:
		http.NotFound(w, r)
	}
}

func esc(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
