package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Settings saved before multi-domain support move into Zones.
func TestMigrateSingleZone(t *testing.T) {
	dir := t.TempDir()
	old := `{"domains":["example.com"],"cloudflare":{"token":"tok","account_id":"a1","zone_id":"z1",
		"worker_name":"mailer-inbound-example-com","spf_ip":"203.0.113.50"}}`
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := m.Get()
	z := cfg.Cloudflare.Zones["example.com"]
	if !cfg.Connected() || z.ID != "z1" || z.AccountID != "a1" || z.WorkerName != "mailer-inbound-example-com" || z.SPFIP != "203.0.113.50" {
		t.Errorf("not migrated: %+v", cfg.Cloudflare)
	}
	if cfg.Cloudflare.ZoneID != "" || cfg.Cloudflare.WorkerName != "" || cfg.Cloudflare.SPFIP != "" {
		t.Errorf("old fields kept: %+v", cfg.Cloudflare)
	}

	// Get returns a copy: changing it doesn't touch the saved settings.
	cfg.Cloudflare.Zones["other.com"] = Zone{}
	if len(m.Get().Cloudflare.Zones) != 1 {
		t.Error("Get shares the Zones map")
	}
}
