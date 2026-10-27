// Package tailscale drives the Tailscale client running beside mailer in
// the container to publish the IMAP and submission ports through Tailscale
// Funnel.
package tailscale

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// find locates the tailscale CLI.
func find() (string, error) {
	p, err := exec.LookPath("tailscale")
	if err != nil {
		return "", errors.New("Tailscale is not installed")
	}
	return p, nil
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

// ActionNeeded means the user must open URL once (to allow Funnel on
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
// EnableFunnel then returns an ActionNeeded with the approval link for the
// dashboard to show.
func EnableFunnel(ctx context.Context, imapPort, subPort int) error {
	for _, f := range []struct{ public, local int }{{443, imapPort}, {8443, subPort}} {
		// Waiting for approval, the CLI either exits after printing the
		// link or blocks until approved, so it's stopped after 20s.
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		out, err := run(cctx, "funnel", "--bg", "--yes", "--proxy-protocol=2",
			fmt.Sprintf("--tls-terminated-tcp=%d", f.public), fmt.Sprintf("tcp://127.0.0.1:%d", f.local))
		cancel()
		text := strings.TrimSpace(string(out))
		if u := urlRe.FindString(text); u != "" {
			return &ActionNeeded{URL: u, Message: "Allow Funnel for this machine in Tailscale, then click Enable again"}
		}
		if err != nil {
			return funnelError(text, err)
		}
	}
	return nil
}

func funnelError(text string, err error) error {
	switch {
	case strings.Contains(text, "flag provided but not defined"):
		return errors.New("this Tailscale is too old for Funnel's TCP forwarding; rebuild the container to update it")
	case text == "":
		return err
	}
	return errors.New(text)
}
