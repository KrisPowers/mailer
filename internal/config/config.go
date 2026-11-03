// Package config holds the server's settings. They live in
// <data>/settings.json and are edited through the admin UI, not by hand.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mailer/internal/users"
)

type Config struct {
	// Domains are the email domains we receive for; Domains[0] is primary.
	Domains []string `json:"domains"`
	// Hostname is used in SMTP greetings and Received headers, and gets an
	// A record so receivers can resolve our HELO name.
	Hostname string `json:"hostname"`

	DKIMSelector    string `json:"dkim_selector"`
	MaxMessageBytes int64  `json:"max_message_bytes"`

	// Aliases maps an address to a local user; "@example.com" is a catch-all.
	Aliases map[string]string `json:"aliases"`

	RejectDMARCFailures bool `json:"reject_dmarc_failures"`
	SaveSentCopy        bool `json:"save_sent_copy"`

	Cloudflare Cloudflare `json:"cloudflare"`
	Tailscale  Tailscale  `json:"tailscale"`
	Ports      Ports      `json:"ports"`
	Remote     Remote     `json:"remote"`
	Webmail    Webmail    `json:"webmail"`

	// Trace logs every command mail apps send (passwords hidden), and
	// IMAPCaps replaces the IMAP extensions advertised; both are for
	// tracking down a mail app that misbehaves.
	Trace    bool   `json:"trace,omitempty"`
	IMAPCaps string `json:"imap_caps,omitempty"`
}

type Cloudflare struct {
	// Token is an API token, or the access token from signing in with
	// Cloudflare, which RefreshToken renews before TokenExpiry.
	Token        string    `json:"token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	TokenExpiry  time.Time `json:"token_expiry,omitempty"`
	// The tunnel lives in the primary domain's account and zone.
	AccountID     string    `json:"account_id"`
	TunnelID      string    `json:"tunnel_id"`
	TunnelToken   string    `json:"tunnel_token"`
	InboundHost   string    `json:"inbound_host"`   // e.g. mail-in.example.com
	InboundSecret string    `json:"inbound_secret"` // shared with the Worker
	ConnectedAt   time.Time `json:"connected_at"`
	// Zones holds each connected domain's Cloudflare state, keyed by domain.
	Zones map[string]Zone `json:"zones"`
	// SMTPToken is an API token with Email Sending permission. When set,
	// mail apps send through Cloudflare's SMTP relay on port 465, which
	// Outlook's cloud sync reaches; it can't send on Funnel's port 8443.
	SMTPToken string `json:"smtp_token,omitempty"`

	// Single-domain fields from before Zones, moved into Zones on load.
	ZoneID     string `json:"zone_id,omitempty"`
	WorkerName string `json:"worker_name,omitempty"`
	SPFIP      string `json:"spf_ip,omitempty"`
}

// Zone is one domain's Cloudflare zone.
type Zone struct {
	ID          string    `json:"id"`
	AccountID   string    `json:"account_id"`
	WorkerName  string    `json:"worker_name"`      // Email Worker the catch-all rule points at
	SPFIP       string    `json:"spf_ip,omitempty"` // home IP older versions put in SPF; removed on the next DNS sync
	ConnectedAt time.Time `json:"connected_at"`
}

type Tailscale struct {
	DNSName   string    `json:"dns_name"` // e.g. host.tailnet.ts.net
	EnabledAt time.Time `json:"enabled_at"`
	// Hostname is this server's name on the tailnet. AuthKey, when set,
	// signs it in without the link the dashboard shows.
	Hostname string `json:"hostname,omitempty"`
	AuthKey  string `json:"auth_key,omitempty"`
}

// Remote serves the dashboard on <Subdomain>.<primary domain> through the
// Cloudflare Tunnel, so it can be reached from anywhere. Signing in still
// needs a device an admin approved.
type Remote struct {
	Enabled   bool   `json:"enabled"`
	Subdomain string `json:"subdomain"`
}

// DashboardHost is the dashboard's public address, or "" when it's only
// reachable from this computer.
func (c *Config) DashboardHost() string {
	if !c.Remote.Enabled || c.Remote.Subdomain == "" || c.Domain() == "" {
		return ""
	}
	return c.Remote.Subdomain + "." + c.Domain()
}

// Webmail serves the webmail on <Subdomain>.<primary domain> through the
// Cloudflare Tunnel, so accounts can read their mail from anywhere. Each
// visit still signs in with the account's address and password.
type Webmail struct {
	Enabled   bool   `json:"enabled"`
	Subdomain string `json:"subdomain"`
}

// WebmailHost is the webmail's public address, or "" when it's only
// reachable from this computer.
func (c *Config) WebmailHost() string {
	if !c.Webmail.Enabled || c.Webmail.Subdomain == "" || c.Domain() == "" {
		return ""
	}
	return c.Webmail.Subdomain + "." + c.Domain()
}

// Ports are loopback-only listeners. Nothing listens publicly: Cloudflare
// Tunnel and Tailscale Funnel connect to these locally.
type Ports struct {
	Admin      int `json:"admin"`
	Webmail    int `json:"webmail"`
	Inbound    int `json:"inbound"`    // HTTP endpoint the Worker posts mail to
	IMAP       int `json:"imap"`       // Funnel :443 -> here (TLS terminated by Tailscale)
	Submission int `json:"submission"` // Funnel :8443 -> here
}

// Funnel ports clients connect to.
const (
	PublicIMAPPort       = 443
	PublicSubmissionPort = 8443
)

// Cloudflare's SMTP relay for Email Sending, and the username it takes.
const (
	RelayHost = "smtp.mx.cloudflare.net"
	RelayPort = 465
	RelayUser = "api_token"
)

// SMTPServer is the outgoing server mail apps use: Cloudflare's relay when
// a relay token is set, otherwise this server through Funnel.
func (c *Config) SMTPServer() (host string, port int, relay bool) {
	if c.Cloudflare.SMTPToken != "" {
		return RelayHost, RelayPort, true
	}
	return c.Tailscale.DNSName, PublicSubmissionPort, false
}

func (c *Config) IsLocalDomain(domain string) bool {
	domain = strings.ToLower(domain)
	for _, d := range c.Domains {
		if d == domain {
			return true
		}
	}
	return false
}

// Connected reports whether at least one domain is set up in Cloudflare.
func (c *Config) Connected() bool { return len(c.Cloudflare.Zones) > 0 }

// Domain returns the primary domain, or "" before setup.
func (c *Config) Domain() string {
	if len(c.Domains) == 0 {
		return ""
	}
	return c.Domains[0]
}

func (c *Config) applyDefaults() {
	if c.DKIMSelector == "" {
		c.DKIMSelector = "mail"
	}
	if c.MaxMessageBytes == 0 {
		c.MaxMessageBytes = 25 << 20 // Cloudflare Email Routing's limit
	}
	if c.Hostname == "" {
		c.Hostname = "localhost"
	}
	if c.Aliases == nil {
		c.Aliases = map[string]string{}
	}
	if c.Tailscale.Hostname == "" {
		c.Tailscale.Hostname = "mail"
	}
	if c.Remote.Subdomain == "" {
		c.Remote.Subdomain = "mail-admin"
	}
	if c.Ports.Admin == 0 {
		c.Ports.Admin = 8080
	}
	if c.Ports.Webmail == 0 {
		c.Ports.Webmail = 8081
	}
	if c.Webmail.Subdomain == "" {
		c.Webmail.Subdomain = "webmail"
	}
	if c.Ports.Inbound == 0 {
		c.Ports.Inbound = 8025
	}
	if c.Ports.IMAP == 0 {
		c.Ports.IMAP = 1143
	}
	if c.Ports.Submission == 0 {
		c.Ports.Submission = 1587
	}
	cf := &c.Cloudflare
	if cf.Zones == nil {
		cf.Zones = map[string]Zone{}
	}
	if cf.ZoneID != "" && c.Domain() != "" {
		cf.Zones[c.Domain()] = Zone{ID: cf.ZoneID, AccountID: cf.AccountID, WorkerName: cf.WorkerName,
			SPFIP: cf.SPFIP, ConnectedAt: cf.ConnectedAt}
		cf.ZoneID, cf.WorkerName, cf.SPFIP = "", "", ""
	}
	if c.Cloudflare.InboundSecret == "" {
		b := make([]byte, 32)
		rand.Read(b)
		c.Cloudflare.InboundSecret = hex.EncodeToString(b)
	}
}

// Manager guards the settings and persists every change.
type Manager struct {
	path    string
	DataDir string
	mu      sync.RWMutex
	c       Config
}

func Open(dataDir string) (*Manager, error) {
	m := &Manager{path: filepath.Join(dataDir, "settings.json"), DataDir: dataDir}
	b, err := os.ReadFile(m.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &m.c); err != nil {
			return nil, err
		}
	}
	m.c.applyDefaults()
	return m, m.save()
}

// Get returns a copy of the current settings.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c := m.c
	c.Domains = append([]string(nil), m.c.Domains...)
	c.Aliases = maps.Clone(m.c.Aliases)
	c.Cloudflare.Zones = maps.Clone(m.c.Cloudflare.Zones)
	return c
}

// Update applies fn and saves.
func (m *Manager) Update(fn func(*Config)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	fn(&m.c)
	m.c.applyDefaults()
	return m.save()
}

func (m *Manager) save() error {
	b, err := json.MarshalIndent(m.c, "", "  ")
	if err != nil {
		return err
	}
	return users.WriteFileAtomic(m.path, b, 0o600)
}
