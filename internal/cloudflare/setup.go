package cloudflare

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"mailer/internal/config"
)

//go:embed worker/inbound.js
var workerJS string

// Step is one line of the setup checklist shown in the admin UI.
type Step struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// ZoneChoiceError means the account can see several zones and the user must
// pick one.
type ZoneChoiceError struct{ Zones []string }

func (e *ZoneChoiceError) Error() string { return "choose which domain to use" }

// Setup performs the Cloudflare configuration for each domain and the
// periodic DNS upkeep.
type Setup struct {
	Cfg *config.Manager
	// DKIMRecord returns the DKIM TXT value for a domain, creating its key
	// if needed.
	DKIMRecord func(domain string) (string, error)
	Log        *log.Logger

	mu sync.Mutex // one credential renewal at a time
}

const spfInclude = "include:_spf.mx.cloudflare.net"

// workerName is the Email Worker deployed to each Cloudflare account. All
// domains in an account share it.
const workerName = "email-inbound"

// Connect connects zoneName (or the account's only zone) to this server
// with the saved sign-in or API token: tunnel, Email Worker, Email
// Routing, Email Sending, DKIM, DMARC and SPF. The first
// domain connected is the primary: it carries the server's hostname and
// the tunnel's inbound address. Connect is idempotent: running it again
// repairs whatever is missing.
func (s *Setup) Connect(ctx context.Context, zoneName string) ([]Step, error) {
	var steps []Step
	fail := func(name string, err error) ([]Step, error) {
		steps = append(steps, Step{Name: name, Detail: err.Error()})
		return steps, err
	}
	done := func(name, detail string) { steps = append(steps, Step{Name: name, OK: true, Detail: detail}) }

	c, err := s.client(ctx)
	if err != nil {
		return fail("Sign in to Cloudflare", err)
	}
	zones, err := c.Zones(ctx)
	if err != nil {
		return fail("Find domain", err)
	}
	var zone *Zone
	for i := range zones {
		if zoneName == "" && len(zones) == 1 || strings.EqualFold(zones[i].Name, zoneName) {
			zone = &zones[i]
		}
	}
	if zone == nil {
		if len(zones) == 0 {
			return fail("Find domain", errors.New("no domains on this Cloudflare account; add your domain to Cloudflare first"))
		}
		var names []string
		for _, z := range zones {
			names = append(names, z.Name)
		}
		return steps, &ZoneChoiceError{Zones: names}
	}
	domain := strings.ToLower(zone.Name)
	acct := zone.Account.ID
	done("Find domain", domain)

	cfg := s.Cfg.Get()
	primary := !cfg.Connected() || domain == cfg.Domain()
	tunnelAcct, tunnelZone, inboundHost := cfg.Cloudflare.AccountID, cfg.Cloudflare.Zones[cfg.Domain()].ID, cfg.Cloudflare.InboundHost
	if primary {
		tunnelAcct, tunnelZone, inboundHost = acct, zone.ID, "mail-in."+domain
	}
	domains := cfg.Domains
	if !cfg.IsLocalDomain(domain) {
		domains = append(domains, domain)
	}
	tunnelID, tunnelToken, err := s.ensureTunnel(ctx, c, tunnelAcct, tunnelZone, inboundHost, domains)
	if err != nil {
		return fail("Create tunnel", err)
	}
	done("Create tunnel", inboundHost)

	worker := cfg.Cloudflare.Zones[domain].WorkerName
	if worker == "" {
		worker = workerName
	}
	if err := c.UploadWorker(ctx, acct, worker, workerJS,
		map[string]string{"INBOUND_URL": "https://" + inboundHost + "/inbound"},
		map[string]string{"INBOUND_SECRET": cfg.Cloudflare.InboundSecret}); err != nil {
		return fail("Deploy email Worker", err)
	}
	done("Deploy email Worker", worker)

	if err := c.EnableEmailRouting(ctx, zone.ID, domain); err != nil {
		return fail("Turn on Email Routing", fmt.Errorf("%w (if this domain already has MX records from another mail provider, remove them first)", err))
	}
	if err := c.RouteAllToWorker(ctx, zone.ID, worker); err != nil {
		return fail("Turn on Email Routing", err)
	}
	done("Turn on Email Routing", "all mail for @"+domain+" → this server")

	now := time.Now()
	if err := s.Cfg.Update(func(c *config.Config) {
		if !c.IsLocalDomain(domain) {
			if primary {
				c.Domains = append([]string{domain}, c.Domains...)
			} else {
				c.Domains = append(c.Domains, domain)
			}
		}
		z := c.Cloudflare.Zones[domain]
		z.ID, z.AccountID, z.WorkerName = zone.ID, acct, worker
		if z.ConnectedAt.IsZero() {
			z.ConnectedAt = now
		}
		c.Cloudflare.Zones[domain] = z
		c.Cloudflare.TunnelID = tunnelID
		c.Cloudflare.TunnelToken = tunnelToken
		if primary {
			c.Hostname = "mail." + domain
			c.Cloudflare.AccountID = acct
			c.Cloudflare.InboundHost = inboundHost
			if c.Cloudflare.ConnectedAt.IsZero() {
				c.Cloudflare.ConnectedAt = now
			}
		}
	}); err != nil {
		return fail("Save settings", err)
	}

	if err := s.publishAuthRecords(ctx, c, zone.ID, domain); err != nil {
		return fail("Publish DKIM / DMARC", err)
	}
	done("Publish DKIM / DMARC", "")

	if err := s.onboardSending(ctx, c, zone.ID, domain); err != nil {
		return fail("Turn on Email Sending", fmt.Errorf("%w (Email Sending needs the Workers Paid plan on this Cloudflare account)", err))
	}
	done("Turn on Email Sending", "outgoing mail leaves through Cloudflare")

	if err := s.syncDomain(ctx, s.Cfg.Get(), domain); err != nil {
		return fail("Publish SPF", err)
	}
	done("Publish SPF", "")

	if err := s.syncClientHosts(ctx, c, s.Cfg.Get()); err != nil {
		return fail("Publish mail app settings", err)
	}
	done("Publish mail app settings", strings.Join(clientHosts(domain), ", "))
	s.Log.Printf("cloudflare: connected %s", domain)
	return steps, nil
}

// ensureTunnel reuses our tunnel if it still exists in acct (or creates
// one) and routes host, and the domains' mail app settings names, to the
// local inbound port through it.
func (s *Setup) ensureTunnel(ctx context.Context, c *Client, acct, zoneID, host string, domains []string) (id, token string, err error) {
	cfg := s.Cfg.Get()
	var tunnel *Tunnel
	if cfg.Cloudflare.TunnelID != "" && cfg.Cloudflare.AccountID == acct {
		if t, err := c.GetTunnel(ctx, acct, cfg.Cloudflare.TunnelID); err == nil && t.DeletedAt == nil {
			tunnel = t
		}
	}
	if tunnel == nil {
		suffix := make([]byte, 3)
		rand.Read(suffix)
		if tunnel, err = c.CreateTunnel(ctx, acct, "mail-"+hex.EncodeToString(suffix)); err != nil {
			return "", "", err
		}
	}
	if token, err = c.TunnelToken(ctx, acct, tunnel.ID); err != nil {
		return "", "", err
	}
	if err := c.SetTunnelIngress(ctx, acct, tunnel.ID, ingressHosts(host, domains), inboundService(cfg)); err != nil {
		return "", "", err
	}
	on := true
	if err := c.UpsertRecord(ctx, zoneID, Record{Type: "CNAME", Name: host,
		Content: tunnel.ID + ".cfargotunnel.com", TTL: 1, Proxied: &on}, nil); err != nil {
		return "", "", err
	}
	return tunnel.ID, token, nil
}

// clientHosts are the names mail apps look up to find a domain's server
// settings from an address alone: Autodiscover (Outlook) and autoconfig
// (Thunderbird and others). The tunnel carries them to this server.
func clientHosts(domain string) []string {
	return []string{"autodiscover." + domain, "autoconfig." + domain}
}

// ingressHosts is everything the tunnel serves: the Worker's inbound
// address and every domain's mail app settings names.
func ingressHosts(inbound string, domains []string) []string {
	hosts := []string{inbound}
	for _, d := range domains {
		hosts = append(hosts, clientHosts(d)...)
	}
	return hosts
}

func inboundService(cfg config.Config) string {
	return fmt.Sprintf("http://127.0.0.1:%d", cfg.Ports.Inbound)
}

// syncClientHosts routes every domain's mail app settings names through
// the tunnel, changing the tunnel's routes only when they differ.
func (s *Setup) syncClientHosts(ctx context.Context, c *Client, cfg config.Config) error {
	cf := cfg.Cloudflare
	want := ingressHosts(cf.InboundHost, cfg.Domains)
	have, err := c.TunnelIngressHosts(ctx, cf.AccountID, cf.TunnelID)
	if err != nil {
		return err
	}
	if !slices.Equal(have, want) {
		if err := c.SetTunnelIngress(ctx, cf.AccountID, cf.TunnelID, want, inboundService(cfg)); err != nil {
			return err
		}
	}
	on := true
	for _, d := range cfg.Domains {
		z, ok := cf.Zones[d]
		if !ok {
			continue
		}
		for _, h := range clientHosts(d) {
			if err := c.UpsertRecord(ctx, z.ID, Record{Type: "CNAME", Name: h,
				Content: cf.TunnelID + ".cfargotunnel.com", TTL: 1, Proxied: &on}, nil); err != nil {
				return fmt.Errorf("%s: %w", h, err)
			}
		}
	}
	return nil
}

// Zones lists the domains the saved sign-in or API token can see.
func (s *Setup) Zones(ctx context.Context) ([]Zone, error) {
	c, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	return c.Zones(ctx)
}

// Disconnect stops receiving mail for a secondary domain: its catch-all
// rule is turned off in Cloudflare, and the domain and its aliases are
// removed here. DNS records and accounts are left alone. The removal
// happens even if Cloudflare can't be reached, which the warning reports.
func (s *Setup) Disconnect(ctx context.Context, domain string) (warning string, err error) {
	cfg := s.Cfg.Get()
	domain = strings.ToLower(strings.TrimSpace(domain))
	if !cfg.IsLocalDomain(domain) {
		return "", fmt.Errorf("%s isn't one of your domains", domain)
	}
	if domain == cfg.Domain() {
		return "", errors.New("the primary domain carries the server's hostname and tunnel, so it can't be removed")
	}
	var cfErr error
	if z, ok := cfg.Cloudflare.Zones[domain]; ok {
		var c *Client
		if c, cfErr = s.client(ctx); cfErr == nil {
			cfErr = c.DisableCatchAll(ctx, z.ID)
		}
	}
	if err := s.Cfg.Update(func(c *config.Config) {
		c.Domains = slices.DeleteFunc(c.Domains, func(d string) bool { return d == domain })
		delete(c.Cloudflare.Zones, domain)
		for alias := range c.Aliases {
			if strings.HasSuffix(alias, "@"+domain) {
				delete(c.Aliases, alias)
			}
		}
	}); err != nil {
		return "", err
	}
	s.Log.Printf("cloudflare: removed %s", domain)
	if cfErr != nil {
		return "Email Routing is still on in Cloudflare: " + cfErr.Error(), nil
	}
	return "", nil
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `" "`, "")
	}
	return s
}

const defaultDMARC = "v=DMARC1; p=quarantine; adkim=r; aspf=r"

func (s *Setup) publishAuthRecords(ctx context.Context, c *Client, zoneID, domain string) error {
	cfg := s.Cfg.Get()
	dkim, err := s.DKIMRecord(domain)
	if err != nil {
		return err
	}
	if err := c.UpsertRecord(ctx, zoneID, Record{Type: "TXT", Name: cfg.DKIMSelector + "._domainkey." + domain,
		Content: dkim, TTL: 1}, nil); err != nil {
		return err
	}
	// Add a DMARC policy only if the domain doesn't already have one.
	recs, err := c.ListRecords(ctx, zoneID, "TXT", "_dmarc."+domain)
	if err != nil {
		return err
	}
	for _, r := range recs {
		if strings.HasPrefix(unquote(r.Content), "v=DMARC1") {
			return nil
		}
	}
	_, err = c.CreateRecord(ctx, zoneID, Record{Type: "TXT", Name: "_dmarc." + domain, Content: defaultDMARC, TTL: 1})
	return err
}

// onboardSending turns on Email Sending for domain and publishes the DNS
// records it asks for.
func (s *Setup) onboardSending(ctx context.Context, c *Client, zoneID, domain string) error {
	sds, err := c.SendingDomains(ctx, zoneID)
	if err != nil {
		return err
	}
	var sd *SendingDomain
	if i := slices.IndexFunc(sds, func(d SendingDomain) bool { return strings.EqualFold(d.Name, domain) }); i >= 0 {
		sd = &sds[i]
	} else if sd, err = c.EnableSending(ctx, zoneID, domain); err != nil {
		return err
	}
	recs, err := c.SendingDNS(ctx, zoneID, sd.Tag)
	if err != nil {
		return err
	}
	for _, r := range recs {
		if err := publishSendingRecord(ctx, c, zoneID, domain, r); err != nil {
			return err
		}
	}
	return nil
}

// publishSendingRecord adds a record Email Sending asked for, replacing an
// older value of the same kind (same name and v= tag). It leaves the
// domain's own SPF, which SyncDNS maintains, and an existing DMARC policy
// alone.
func publishSendingRecord(ctx context.Context, c *Client, zoneID, domain string, r Record) error {
	r.ID, r.Proxied, r.Content = "", nil, unquote(r.Content)
	switch {
	case r.Name == "" || r.Name == "@":
		r.Name = domain
	case r.Name != domain && !strings.HasSuffix(r.Name, "."+domain):
		r.Name += "." + domain
	}
	if r.TTL == 0 {
		r.TTL = 1
	}
	existing, err := c.ListRecords(ctx, zoneID, r.Type, r.Name)
	if err != nil {
		return err
	}
	tag := tagOf(r.Content)
	for _, old := range existing {
		if compact(unquote(old.Content)) == compact(r.Content) || tag == "v=dmarc1" && tagOf(unquote(old.Content)) == tag {
			return nil
		}
	}
	if r.Type != "TXT" || tag == "" {
		_, err := c.CreateRecord(ctx, zoneID, r)
		return err
	}
	if tag == "v=spf1" && r.Name == domain {
		return nil
	}
	return c.UpsertRecord(ctx, zoneID, r, func(old Record) bool { return tagOf(unquote(old.Content)) == tag })
}

// tagOf returns a TXT record's lowercased "v=" tag, or "".
func tagOf(v string) string {
	f := strings.FieldsFunc(v, func(r rune) bool { return r == ';' || r == ' ' })
	if len(f) == 0 || !strings.HasPrefix(strings.ToLower(f[0]), "v=") {
		return ""
	}
	return strings.ToLower(f[0])
}

// SyncDNS keeps every domain's SPF record and mail app settings names in
// place. Mail leaves through Cloudflare Email Sending, so this machine's
// IP is not published: an address older versions put in SPF, and the A
// record they pointed the server's hostname at it with, are removed.
func (s *Setup) SyncDNS(ctx context.Context) error {
	cfg := s.Cfg.Get()
	if !cfg.Connected() {
		return errors.New("Cloudflare is not connected")
	}
	var errs []error
	for _, d := range cfg.Domains {
		if err := s.syncDomain(ctx, cfg, d); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", d, err))
		}
	}
	c, err := s.client(ctx)
	if err == nil {
		err = s.syncClientHosts(ctx, c, cfg)
	}
	if err != nil {
		errs = append(errs, fmt.Errorf("mail app settings: %w", err))
	}
	return errors.Join(errs...)
}

func (s *Setup) syncDomain(ctx context.Context, cfg config.Config, domain string) error {
	z, ok := cfg.Cloudflare.Zones[domain]
	if !ok {
		return nil
	}
	c, err := s.client(ctx)
	if err != nil {
		return err
	}
	if domain == cfg.Domain() && z.SPFIP != "" {
		recs, err := c.ListRecords(ctx, z.ID, "A", cfg.Hostname)
		if err != nil {
			return err
		}
		for _, r := range recs {
			if r.Content == z.SPFIP {
				if err := c.DeleteRecord(ctx, z.ID, r.ID); err != nil {
					return err
				}
				s.Log.Printf("dns: removed %s A %s", cfg.Hostname, r.Content)
			}
		}
	}
	recs, err := c.ListRecords(ctx, z.ID, "TXT", domain)
	if err != nil {
		return err
	}
	existing := ""
	for _, r := range recs {
		if v := unquote(r.Content); strings.HasPrefix(v, "v=spf1") {
			existing = v
			break
		}
	}
	want := MergeSPF(existing, z.SPFIP)
	if want != existing {
		if err := c.UpsertRecord(ctx, z.ID, Record{Type: "TXT", Name: domain, Content: want, TTL: 1},
			func(r Record) bool { return strings.HasPrefix(unquote(r.Content), "v=spf1") }); err != nil {
			return err
		}
		s.Log.Printf("dns: SPF for %s is now %q", domain, want)
	}
	if z.SPFIP != "" {
		s.Cfg.Update(func(c *config.Config) {
			if cur, ok := c.Cloudflare.Zones[domain]; ok {
				cur.SPFIP = ""
				c.Cloudflare.Zones[domain] = cur
			}
		})
	}
	return nil
}

// MergeSPF returns existing with Cloudflare's include present and oldIP,
// the address older versions published, dropped. Other mechanisms are
// preserved.
func MergeSPF(existing, oldIP string) string {
	terms := strings.Fields(existing)
	if len(terms) == 0 || !strings.EqualFold(terms[0], "v=spf1") {
		terms = []string{"v=spf1", spfInclude, "~all"}
	}
	out := []string{"v=spf1"}
	all := "~all"
	redirect := ""
	hasInclude := false
	for _, t := range terms[1:] {
		lt := strings.ToLower(t)
		switch {
		case oldIP != "" && lt == "ip4:"+oldIP:
			continue
		case lt == "all" || (len(lt) == 4 && strings.HasSuffix(lt, "all")):
			all = t
			continue
		case strings.HasPrefix(lt, "redirect="):
			redirect = t // modifiers go after mechanisms
			continue
		case lt == spfInclude:
			hasInclude = true
		}
		out = append(out, t)
	}
	if !hasInclude {
		out = append(out, spfInclude)
	}
	out = append(out, all)
	if redirect != "" {
		// "all" makes redirect unreachable, so a redirect replaces it.
		out[len(out)-1] = redirect
	}
	return strings.Join(out, " ")
}

// Status is a snapshot of the Cloudflare side for the admin UI.
type Status struct {
	Connected     bool           `json:"connected"`
	Domain        string         `json:"domain"` // primary
	TunnelStatus  string         `json:"tunnel_status"`
	TunnelConns   int            `json:"tunnel_connections"`
	PublicHealthy bool           `json:"public_healthy"`
	Domains       []DomainStatus `json:"domains"`
	Error         string         `json:"error,omitempty"`
}

// DomainStatus is one domain's Email Routing state and the DNS records it
// needs to receive and send mail.
type DomainStatus struct {
	Name      string `json:"name"`
	Primary   bool   `json:"primary"`
	AccountID string `json:"account_id"`
	Checks    []Step `json:"checks"`
	Error     string `json:"error,omitempty"`
}

// OK reports whether every check passed.
func (d DomainStatus) OK() bool {
	if d.Error != "" || len(d.Checks) == 0 {
		return false
	}
	for _, c := range d.Checks {
		if !c.OK {
			return false
		}
	}
	return true
}

// CheckStatus queries Cloudflare and probes the public inbound URL.
func (s *Setup) CheckStatus(ctx context.Context, probe func(ctx context.Context, url string) error) Status {
	cfg := s.Cfg.Get()
	st := Status{Domain: cfg.Domain(), Domains: []DomainStatus{}}
	if !cfg.Connected() {
		return st
	}
	st.Connected = true
	c, err := s.client(ctx)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Domains = make([]DomainStatus, len(cfg.Domains))
	var wg sync.WaitGroup
	for i, d := range cfg.Domains {
		wg.Go(func() { st.Domains[i] = s.checkDomain(ctx, c, cfg, d) })
	}
	if t, err := c.GetTunnel(ctx, cfg.Cloudflare.AccountID, cfg.Cloudflare.TunnelID); err != nil {
		st.Error = err.Error()
	} else {
		st.TunnelStatus, st.TunnelConns = t.Status, len(t.Connections)
	}
	st.PublicHealthy = probe(ctx, "https://"+cfg.Cloudflare.InboundHost+"/health") == nil
	wg.Wait()
	return st
}

// checkDomain verifies a domain's Email Routing and its MX, SPF, DKIM and
// DMARC records, reading the records from Cloudflare rather than DNS so
// changes show up without waiting for caches.
func (s *Setup) checkDomain(ctx context.Context, c *Client, cfg config.Config, domain string) DomainStatus {
	ds := DomainStatus{Name: domain, Primary: domain == cfg.Domain(), Checks: []Step{}}
	z, ok := cfg.Cloudflare.Zones[domain]
	if !ok {
		ds.Error = "not connected to Cloudflare"
		return ds
	}
	ds.AccountID = z.AccountID
	add := func(name string, ok bool, detail string) { ds.Checks = append(ds.Checks, Step{name, ok, detail}) }
	txt := func(name, prefix string) ([]string, error) {
		recs, err := c.ListRecords(ctx, z.ID, "TXT", name)
		var out []string
		for _, r := range recs {
			if v := unquote(r.Content); strings.HasPrefix(v, prefix) {
				out = append(out, v)
			}
		}
		return out, err
	}

	if er, err := c.EmailRoutingStatus(ctx, z.ID); err != nil {
		add("Email Routing", false, err.Error())
	} else {
		add("Email Routing", er.Enabled && er.Status == "ready", orNone(er.Status))
	}

	if w, enabled, err := c.CatchAllWorker(ctx, z.ID); err != nil {
		add("Catch-all to Worker", false, err.Error())
	} else {
		detail := "points at " + orNone(w)
		if !enabled {
			detail = "turned off"
		}
		add("Catch-all to Worker", enabled && w == z.WorkerName, detail)
	}

	if mx, err := c.ListRecords(ctx, z.ID, "MX", domain); err != nil {
		add("MX", false, err.Error())
	} else {
		var hosts []string
		ok := len(mx) > 0
		for _, r := range mx {
			hosts = append(hosts, r.Content)
			ok = ok && strings.HasSuffix(strings.TrimSuffix(r.Content, "."), ".mx.cloudflare.net")
		}
		detail := strings.Join(hosts, ", ")
		if len(mx) == 0 {
			detail = "missing"
		} else if !ok {
			detail = "not all Cloudflare: " + detail
		}
		add("MX", ok, detail)
	}

	if spf, err := txt(domain, "v=spf1"); err != nil {
		add("SPF", false, err.Error())
	} else {
		switch {
		case len(spf) == 0:
			add("SPF", false, "missing")
		case len(spf) > 1:
			add("SPF", false, "more than one SPF record")
		default:
			add("SPF", strings.Contains(spf[0], spfInclude), spf[0])
		}
	}

	if s.DKIMRecord != nil {
		name := cfg.DKIMSelector + "._domainkey." + domain
		want, err := s.DKIMRecord(domain)
		got, err2 := txt(name, "")
		switch {
		case err != nil || err2 != nil:
			add("DKIM", false, errors.Join(err, err2).Error())
		case slices.ContainsFunc(got, func(v string) bool { return compact(v) == compact(want) }):
			add("DKIM", true, name)
		case len(got) == 0:
			add("DKIM", false, name+" missing")
		default:
			add("DKIM", false, name+" doesn't match this server's key")
		}
	}

	if sds, err := c.SendingDomains(ctx, z.ID); err != nil {
		add("Email Sending", false, err.Error())
	} else if slices.ContainsFunc(sds, func(d SendingDomain) bool { return strings.EqualFold(d.Name, domain) && d.Enabled }) {
		add("Email Sending", true, "enabled")
	} else {
		add("Email Sending", false, "not enabled for "+domain)
	}

	missing := []string{}
	for _, h := range clientHosts(domain) {
		recs, err := c.ListRecords(ctx, z.ID, "CNAME", h)
		if err != nil {
			missing = append(missing, h+": "+err.Error())
		} else if !slices.ContainsFunc(recs, func(r Record) bool {
			return strings.TrimSuffix(r.Content, ".") == cfg.Cloudflare.TunnelID+".cfargotunnel.com"
		}) {
			missing = append(missing, h)
		}
	}
	if len(missing) == 0 {
		add("Mail app settings", true, strings.Join(clientHosts(domain), ", "))
	} else {
		add("Mail app settings", false, "not routed to this server: "+strings.Join(missing, ", "))
	}

	switch dmarc, err := txt("_dmarc."+domain, "v=DMARC1"); {
	case err != nil:
		add("DMARC", false, err.Error())
	case len(dmarc) == 0:
		add("DMARC", false, "missing")
	case len(dmarc) > 1:
		add("DMARC", false, "more than one DMARC record")
	default:
		add("DMARC", true, dmarc[0])
	}
	return ds
}

func compact(s string) string { return strings.Join(strings.Fields(s), "") }

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}
