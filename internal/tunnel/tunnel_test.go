package tunnel

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDownload fetches the real cloudflared release for this platform.
// It needs network access, so it only runs with MAILER_NET_TESTS=1.
func TestDownload(t *testing.T) {
	if os.Getenv("MAILER_NET_TESTS") == "" {
		t.Skip("set MAILER_NET_TESTS=1 to run")
	}
	dest := filepath.Join(t.TempDir(), exeName())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := download(ctx, dest); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(dest, "--version").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "cloudflared version") {
		t.Fatalf("downloaded binary doesn't run: %v %s", err, out)
	}
	t.Logf("%s", out)
}
