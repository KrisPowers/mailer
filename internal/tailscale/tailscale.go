// Package tailscale drives the local Tailscale client to publish the IMAP and
// submission ports through Tailscale Funnel.
package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// find locates the tailscale CLI, including the default install locations
// that aren't on PATH.
func find() (string, error) {
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p, nil
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/Applications/Tailscale.app/Contents/MacOS/Tailscale", "/usr/local/bin/tailscale", "/opt/homebrew/bin/tailscale"}
	case "windows":
		candidates = []string{os.Getenv("ProgramFiles") + `\Tailscale\tailscale.exe`, `C:\Program Files\Tailscale\tailscale.exe`}
	default:
		candidates = []string{"/usr/bin/tailscale", "/usr/local/bin/tailscale", "/usr/sbin/tailscale"}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", errors.New("Tailscale is not installed")
}

func run(ctx context.Context, args ...string) ([]byte, error) {
	bin, err := find()
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	return out.Bytes(), err
}

type Status struct {
	Installed bool   `json:"installed"`
	Running   bool   `json:"running"` // logged in and connected
	State     string `json:"state"`
	DNSName   string `json:"dns_name"`
	LoginURL  string `json:"login_url,omitempty"`
	Error     string `json:"error,omitempty"`

	FunnelIMAP       bool `json:"funnel_imap"`
	FunnelSubmission bool `json:"funnel_submission"`
}

// GetStatus reports the Tailscale client state and whether our Funnel
// forwards are active.
func GetStatus(ctx context.Context, imapPort, subPort int) Status {
	var st Status
	if _, err := find(); err != nil {
		return st
	}
	st.Installed = true
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := run(ctx, "status", "--json")
	var s struct {
		BackendState string `json:"BackendState"`
		AuthURL      string `json:"AuthURL"`
		Self         struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if jerr := json.Unmarshal(out, &s); jerr != nil {
		if err != nil {
			st.Error = strings.TrimSpace(string(out))
		}
		return st
	}
	st.State = s.BackendState
	st.Running = s.BackendState == "Running"
	st.DNSName = strings.TrimSuffix(s.Self.DNSName, ".")
	st.LoginURL = s.AuthURL

	if st.Running {
		out, err := run(ctx, "funnel", "status", "--json")
		if err == nil {
			st.FunnelIMAP = funnelActive(out, 443, imapPort)
			st.FunnelSubmission = funnelActive(out, 8443, subPort)
		}
	}
	return st
}

// funnelActive checks the serve config JSON for a public TCP forward from
// publicPort to our local port.
func funnelActive(cfgJSON []byte, publicPort, localPort int) bool {
	var cfg struct {
		TCP map[string]struct {
			TCPForward   string `json:"TCPForward"`
			TerminateTLS string `json:"TerminateTLS"`
		} `json:"TCP"`
		AllowFunnel map[string]bool `json:"AllowFunnel"`
	}
	if json.Unmarshal(cfgJSON, &cfg) != nil {
		return false
	}
	h, ok := cfg.TCP[fmt.Sprint(publicPort)]
	if !ok || !strings.HasSuffix(h.TCPForward, fmt.Sprintf(":%d", localPort)) || h.TerminateTLS == "" {
		return false
	}
	for k, v := range cfg.AllowFunnel {
		if v && strings.HasSuffix(k, fmt.Sprintf(":%d", publicPort)) {
			return true
		}
	}
	return false
}

var urlRe = regexp.MustCompile(`https://login\.tailscale\.com/\S+`)

// ActionNeeded means the user must open URL once (e.g. to allow Funnel on
// their tailnet), after which EnableFunnel should be retried.
type ActionNeeded struct {
	URL     string
	Message string
}

func (e *ActionNeeded) Error() string { return e.Message }

// EnableFunnel publishes IMAP on :443 and submission on :8443. Tailscale
// terminates TLS with its own certificate and forwards to our loopback
// ports with a PROXY protocol header carrying the client address.
func EnableFunnel(ctx context.Context, imapPort, subPort int) error {
	for _, f := range []struct{ public, local int }{{443, imapPort}, {8443, subPort}} {
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		out, err := run(cctx, "funnel", "--bg", "--yes", "--proxy-protocol=2",
			fmt.Sprintf("--tls-terminated-tcp=%d", f.public), fmt.Sprintf("tcp://127.0.0.1:%d", f.local))
		cancel()
		text := strings.TrimSpace(string(out))
		if u := urlRe.FindString(text); u != "" && (err != nil || strings.Contains(strings.ToLower(text), "enable")) {
			return &ActionNeeded{URL: u, Message: "Allow Funnel for this machine, then click Enable again"}
		}
		if err != nil {
			if strings.Contains(text, "Access denied") || strings.Contains(text, "permission") {
				return fmt.Errorf("%s — run once as admin: tailscale set --operator=%s", text, currentUser())
			}
			if text == "" {
				text = err.Error()
			}
			return errors.New(text)
		}
	}
	return nil
}

func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("USERNAME")
}
