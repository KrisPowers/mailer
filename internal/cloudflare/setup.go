package cloudflare

import (
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"mailer/internal/config"
)

//go:embed worker/inbound.js
var workerJS string

var publicIP = PublicIPv4 // replaced in tests

// Step is one line of the setup checklist shown in the admin UI.
type Step struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// ZoneChoiceError means the token can see several zones and the user must
// pick one.
type ZoneChoiceError struct{ Zones []string }

func (e *ZoneChoiceError) Error() string { return "choose which domain to use" }

// Setup performs the one-time Cloudflare configuration and the periodic DNS
// upkeep.
type Setup struct {
	Cfg *config.Manager
	// DKIMRecord returns the DKIM TXT value for a domain, creating its key
	// if needed.
	DKIMRecord func(domain string) (string, error)
	Log        *log.Logger
}

const spfInclude = "include:_spf.mx.cloudflare.net"

var workerNameRe = regexp.MustCompile(`[^a-z0-9-]+`)

// Connect configures everything Cloudflare-side for zoneName using token.
// It is idempotent: running it again repairs whatever is missing.
func (s *Setup) Connect(ctx context.Context, token, zoneName string) ([]Step, error) {
	var steps []Step
	fail := func(name string, err error) ([]Step, error) {
		steps = append(steps, Step{Name: name, Detail: err.Error()})
		return steps, err
	}
	done := func(name, detail string) { steps = append(steps, Step{Name: name, OK: true, Detail: detail}) }

	c := New(strings.TrimSpace(token))
	if err := c.Verify(ctx); err != nil {
		return fail("Check API token", err)
	}
	done("Check API token", "")

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
			return fail("Find domain", errors.New("the token can't see any domains; give it access to your zone"))
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
	inboundHost := "mail-in." + domain

	// Tunnel: reuse ours if it still exists.
	var tunnel *Tunnel
	if cfg.Cloudflare.TunnelID != "" && cfg.Cloudflare.AccountID == acct {
		if t, err := c.GetTunnel(ctx, acct, cfg.Cloudflare.TunnelID); err == nil && t.DeletedAt == nil {
			tunnel = t
		}
	}
	if tunnel == nil {
		suffix := make([]byte, 3)
		rand.Read(suffix)
		if tunnel, err = c.CreateTunnel(ctx, acct, "mailer-"+domain+"-"+hex.EncodeToString(suffix)); err != nil {
			return fail("Create tunnel", err)
		}
	}
	tunnelToken, err := c.TunnelToken(ctx, acct, tunnel.ID)
	if err != nil {
		return fail("Create tunnel", err)
	}
	if err := c.SetTunnelIngress(ctx, acct, tunnel.ID, inboundHost,
		fmt.Sprintf("http://127.0.0.1:%d", cfg.Ports.Inbound)); err != nil {
		return fail("Create tunnel", err)
	}
	on := true
	if err := c.UpsertRecord(ctx, zone.ID, Record{Type: "CNAME", Name: inboundHost,
		Content: tunnel.ID + ".cfargotunnel.com", TTL: 1, Proxied: &on}, nil); err != nil {
		return fail("Create tunnel", err)
	}
	done("Create tunnel", inboundHost)

	workerName := strings.Trim(workerNameRe.ReplaceAllString("mailer-inbound-"+domain, "-"), "-")
	if len(workerName) > 63 {
		workerName = workerName[:63]
	}
	if err := c.UploadWorker(ctx, acct, workerName, workerJS,
		map[string]string{"INBOUND_URL": "https://" + inboundHost + "/inbound"},
		map[string]string{"INBOUND_SECRET": cfg.Cloudflare.InboundSecret}); err != nil {
		return fail("Deploy email Worker", err)
	}
	done("Deploy email Worker", workerName)

	if err := c.EnableEmailRouting(ctx, zone.ID, domain); err != nil {
		return fail("Turn on Email Routing", fmt.Errorf("%w (if this domain already has MX records from another mail provider, remove them first)", err))
	}
	if err := c.RouteAllToWorker(ctx, zone.ID, workerName); err != nil {
		return fail("Turn on Email Routing", err)
	}
	done("Turn on Email Routing", "all mail for @"+domain+" → this server")

	if err := s.Cfg.Update(func(c *config.Config) {
		if !c.IsLocalDomain(domain) {
			c.Domains = append([]string{domain}, c.Domains...)
		}
		c.Hostname = "mail." + domain
		c.Cloudflare.Token = strings.TrimSpace(token)
		c.Cloudflare.AccountID = acct
		c.Cloudflare.ZoneID = zone.ID
		c.Cloudflare.TunnelID = tunnel.ID
		c.Cloudflare.TunnelToken = tunnelToken
		c.Cloudflare.InboundHost = inboundHost
		c.Cloudflare.WorkerName = workerName
		c.Cloudflare.ConnectedAt = time.Now()
	}); err != nil {
		return fail("Save settings", err)
	}

	if err := s.publishAuthRecords(ctx, c, zone.ID, domain); err != nil {
		return fail("Publish DKIM / DMARC", err)
	}
	done("Publish DKIM / DMARC", "")

	ip, err := s.SyncDNS(ctx)
	if err != nil {
		return fail("Publish sending IP (SPF)", err)
	}
	done("Publish sending IP (SPF)", ip)
	s.Log.Printf("cloudflare: connected %s", domain)
	return steps, nil
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return strings.ReplaceAll(s[1:len(s)-1], `" "`, "")
	}
	return s
}

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
	_, err = c.CreateRecord(ctx, zoneID, Record{Type: "TXT", Name: "_dmarc." + domain,
		Content: "v=DMARC1; p=quarantine; adkim=r; aspf=r", TTL: 1})
	return err
}

// SyncDNS publishes this machine's current public IP as the A record for the
// HELO hostname and in the domain's SPF record, so replies sent directly
// from home authenticate. It returns the IP.
func (s *Setup) SyncDNS(ctx context.Context) (string, error) {
	cfg := s.Cfg.Get()
	if cfg.Cloudflare.ZoneID == "" {
		return "", errors.New("Cloudflare is not connected")
	}
	c := New(cfg.Cloudflare.Token)
	ip, err := publicIP(ctx)
	if err != nil {
		return "", err
	}
	domain := cfg.Domain()
	if _, err := c.UpsertA(ctx, cfg.Cloudflare.ZoneID, cfg.Hostname, ip); err != nil {
		return "", err
	}
	recs, err := c.ListRecords(ctx, cfg.Cloudflare.ZoneID, "TXT", domain)
	if err != nil {
		return "", err
	}
	existing := ""
	for _, r := range recs {
		if v := unquote(r.Content); strings.HasPrefix(v, "v=spf1") {
			existing = v
			break
		}
	}
	want := MergeSPF(existing, ip, cfg.Cloudflare.SPFIP)
	if want != existing {
		if err := c.UpsertRecord(ctx, cfg.Cloudflare.ZoneID, Record{Type: "TXT", Name: domain, Content: want, TTL: 1},
			func(r Record) bool { return strings.HasPrefix(unquote(r.Content), "v=spf1") }); err != nil {
			return "", err
		}
		s.Log.Printf("dns: SPF for %s is now %q", domain, want)
	}
	if cfg.Cloudflare.SPFIP != ip {
		s.Cfg.Update(func(c *config.Config) { c.Cloudflare.SPFIP = ip })
	}
	return ip, nil
}

// MergeSPF returns existing with Cloudflare's include and ip4:newIP present,
// dropping our previously published oldIP. Other mechanisms are preserved.
func MergeSPF(existing, newIP, oldIP string) string {
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
		case oldIP != "" && lt == "ip4:"+oldIP, lt == "ip4:"+newIP:
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
	out = append(out, "ip4:"+newIP, all)
	if redirect != "" {
		// "all" makes redirect unreachable, so a redirect replaces it.
		out[len(out)-1] = redirect
	}
	return strings.Join(out, " ")
}

// Status is a snapshot of the Cloudflare side for the admin UI.
type Status struct {
	Connected     bool   `json:"connected"`
	Domain        string `json:"domain"`
	TunnelStatus  string `json:"tunnel_status"`
	TunnelConns   int    `json:"tunnel_connections"`
	EmailRouting  string `json:"email_routing"`
	CatchAllOK    bool   `json:"catch_all_ok"`
	PublicHealthy bool   `json:"public_healthy"`
	Error         string `json:"error,omitempty"`
}

// CheckStatus queries Cloudflare and probes the public inbound URL.
func (s *Setup) CheckStatus(ctx context.Context, probe func(ctx context.Context, url string) error) Status {
	cfg := s.Cfg.Get()
	st := Status{Domain: cfg.Domain()}
	if cfg.Cloudflare.ZoneID == "" {
		return st
	}
	st.Connected = true
	c := New(cfg.Cloudflare.Token)
	var errs []string
	if t, err := c.GetTunnel(ctx, cfg.Cloudflare.AccountID, cfg.Cloudflare.TunnelID); err != nil {
		errs = append(errs, err.Error())
	} else {
		st.TunnelStatus, st.TunnelConns = t.Status, len(t.Connections)
	}
	if er, err := c.EmailRoutingStatus(ctx, cfg.Cloudflare.ZoneID); err != nil {
		errs = append(errs, err.Error())
	} else {
		st.EmailRouting = er.Status
		if !er.Enabled {
			st.EmailRouting = "disabled"
		}
	}
	if w, enabled, err := c.CatchAllWorker(ctx, cfg.Cloudflare.ZoneID); err != nil {
		errs = append(errs, err.Error())
	} else {
		st.CatchAllOK = enabled && w == cfg.Cloudflare.WorkerName
	}
	st.PublicHealthy = probe(ctx, "https://"+cfg.Cloudflare.InboundHost+"/health") == nil
	if len(errs) > 0 {
		st.Error = strings.Join(errs, "; ")
	}
	return st
}
