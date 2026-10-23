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
}

type Cloudflare struct {
	Token         string    `json:"token"`
	AccountID     string    `json:"account_id"`
	ZoneID        string    `json:"zone_id"`
	TunnelID      string    `json:"tunnel_id"`
	TunnelToken   string    `json:"tunnel_token"`
	InboundHost   string    `json:"inbound_host"`   // e.g. mail-in.example.com
	InboundSecret string    `json:"inbound_secret"` // shared with the Worker
	WorkerName    string    `json:"worker_name"`
	SPFIP         string    `json:"spf_ip"` // IP we last published in SPF
	ConnectedAt   time.Time `json:"connected_at"`
}

type Tailscale struct {
	DNSName   string    `json:"dns_name"` // e.g. host.tailnet.ts.net
	EnabledAt time.Time `json:"enabled_at"`
}

// Ports are loopback-only listeners. Nothing listens publicly: Cloudflare
// Tunnel and Tailscale Funnel connect to these locally.
type Ports struct {
	Admin      int `json:"admin"`
	Inbound    int `json:"inbound"`    // HTTP endpoint the Worker posts mail to
	IMAP       int `json:"imap"`       // Funnel :443 -> here (TLS terminated by Tailscale)
	Submission int `json:"submission"` // Funnel :8443 -> here
}

// Funnel ports clients connect to.
const (
	PublicIMAPPort       = 443
	PublicSubmissionPort = 8443
)

func (c *Config) IsLocalDomain(domain string) bool {
	domain = strings.ToLower(domain)
	for _, d := range c.Domains {
		if d == domain {
			return true
		}
	}
	return false
}

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
	if c.Ports.Admin == 0 {
		c.Ports.Admin = 8080
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
