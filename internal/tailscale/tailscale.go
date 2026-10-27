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

	"mailer/internal/console"
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
	cmd := console.Hide(exec.CommandContext(ctx, bin, args...))
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
//
// The first time, Tailscale asks for Funnel to be allowed on the tailnet:
// its approval page is opened with open, and EnableFunnel keeps trying
// until it's approved, for up to five minutes.
func EnableFunnel(ctx context.Context, imapPort, subPort int, open func(url string)) error {
	deadline := time.Now().Add(5 * time.Minute)
	opened := ""
	for _, f := range []struct{ public, local int }{{443, imapPort}, {8443, subPort}} {
		for {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			out, err := run(cctx, "funnel", "--bg", "--yes", "--proxy-protocol=2",
				fmt.Sprintf("--tls-terminated-tcp=%d", f.public), fmt.Sprintf("tcp://127.0.0.1:%d", f.local))
			cancel()
			text := strings.TrimSpace(string(out))
			u := urlRe.FindString(text)
			if u == "" && err == nil {
				break
			}
			if u == "" {
				return funnelError(text, err)
			}
			// Waiting for approval: the CLI either exits after printing the
			// link or blocks until approved (and is stopped at 30s).
			if u != opened && open != nil {
				open(u)
				opened = u
			}
			if time.Now().After(deadline) {
				return &ActionNeeded{URL: u, Message: "Allow Funnel for this machine in Tailscale, then click Enable again"}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
	}
	return nil
}

func funnelError(text string, err error) error {
	switch {
	case strings.Contains(text, "flag provided but not defined"):
		return errors.New("Tailscale on this computer is too old for Funnel's TCP forwarding; update it from tailscale.com/download, then click Enable again")
	case runtime.GOOS != "windows" && (strings.Contains(text, "Access denied") || strings.Contains(text, "permission")):
		return fmt.Errorf("%s. Run once as admin: tailscale set --operator=%s", text, currentUser())
	case text == "":
		return err
	}
	return errors.New(text)
}

func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return os.Getenv("USERNAME")
}
