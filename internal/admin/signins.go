package admin

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/smtp"
	"sort"
	"strings"
	"time"

	"mailer/internal/audit"
	"mailer/internal/cloudflare"
	"mailer/internal/config"
)

type blockedIP struct {
	IP    string    `json:"ip"`
	Until time.Time `json:"until"`
}

// getSignIns returns recent sign-in attempts by mail apps and the
// addresses currently blocked for too many failures.
func (s *Server) getSignIns(w http.ResponseWriter, r *http.Request) {
	events := s.Audit.Recent()
	if events == nil {
		events = []audit.Event{}
	}
	blocked := []blockedIP{}
	for ip, until := range s.Limiter.BlockedIPs() {
		blocked = append(blocked, blockedIP{ip, until})
	}
	sort.Slice(blocked, func(i, j int) bool { return blocked[i].IP < blocked[j].IP })
	writeJSON(w, map[string]any{"events": events, "blocked": blocked})
}

func (s *Server) unblock(w http.ResponseWriter, r *http.Request) {
	var req struct{ IP string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Limiter.Unblock(req.IP)
	s.Log.Printf("admin: unblocked %s for sign-ins", req.IP)
	writeJSON(w, map[string]bool{"ok": true})
}

// testSignIn signs in the way Outlook or the Gmail app would, through the
// public Funnel address, and reports each step.
func (s *Server) testSignIn(w http.ResponseWriter, r *http.Request) {
	var req struct{ Email, Password string }
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	writeJSON(w, map[string]any{"steps": s.signInSteps(ctx, strings.TrimSpace(req.Email), req.Password)})
}

func (s *Server) signInSteps(ctx context.Context, email, password string) []cloudflare.Step {
	var steps []cloudflare.Step
	step := func(name string, err error, okText string) bool {
		st := cloudflare.Step{Name: name, OK: err == nil, Detail: okText}
		if err != nil {
			st.Detail = err.Error()
		}
		steps = append(steps, st)
		return err == nil
	}

	// The password is checked here first, so a typo isn't counted against
	// this server's own address by the rate limit.
	var err error
	switch _, ok := s.Users.Authenticate(email, password); {
	case !s.Users.Exists(email):
		err = fmt.Errorf("no account %q; the username is the full email address", email)
	case !ok:
		err = errors.New("wrong password for this account")
	}
	if !step("Account and password", err, "match") {
		return steps
	}

	host := s.Cfg.Get().Tailscale.DNSName
	if c := s.currentChecks(); c != nil && c.Tailscale.DNSName != "" {
		host = c.Tailscale.DNSName
	}
	if host == "" {
		step("Funnel address", errors.New("Tailscale Funnel isn't enabled yet"), "")
		return steps
	}
	step("Funnel address", nil, host)
	step(fmt.Sprintf("Sign in to IMAP at %s:%d", host, config.PublicIMAPPort),
		imapSignIn(ctx, host, email, password), "signed in and opened INBOX")
	step(fmt.Sprintf("Sign in to SMTP at %s:%d", host, config.PublicSubmissionPort),
		smtpSignIn(ctx, host, email, password), "signed in, ready to send")
	return steps
}

// dialFunnel opens a TLS connection to host's public Funnel port and
// checks Tailscale's certificate, as a mail app does.
func dialFunnel(ctx context.Context, host string, port int) (*tls.Conn, error) {
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 15 * time.Second}, Config: &tls.Config{ServerName: host}}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return nil, fmt.Errorf("can't connect with TLS: %w", err)
	}
	c.SetDeadline(time.Now().Add(30 * time.Second))
	return c.(*tls.Conn), nil
}

func imapSignIn(ctx context.Context, host, email, password string) error {
	c, err := dialFunnel(ctx, host, config.PublicIMAPPort)
	if err != nil {
		return err
	}
	defer c.Close()
	br := bufio.NewReader(c)
	greeting, err := br.ReadString('\n')
	if err != nil || !strings.HasPrefix(greeting, "* OK") {
		return fmt.Errorf("no IMAP greeting (got %q)", strings.TrimSpace(greeting))
	}
	// cmd sends a command and returns its tagged reply.
	cmd := func(tag, line string) (string, error) {
		if _, err := fmt.Fprintf(c, "%s %s\r\n", tag, line); err != nil {
			return "", err
		}
		for {
			l, err := br.ReadString('\n')
			if err != nil {
				return "", err
			}
			if strings.HasPrefix(l, tag+" ") {
				return strings.TrimSpace(strings.TrimPrefix(l, tag+" ")), nil
			}
		}
	}
	quote := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace
	steps := []struct{ tag, line, what string }{
		{"t1", `ID ("name" "Mailer sign-in test")`, "ID"},
		{"t2", `LOGIN "` + quote(email) + `" "` + quote(password) + `"`, "sign-in"},
		{"t3", "SELECT INBOX", "opening INBOX"},
	}
	for _, st := range steps {
		reply, err := cmd(st.tag, st.line)
		if err != nil {
			return fmt.Errorf("%s: %w", st.what, err)
		}
		if !strings.HasPrefix(reply, "OK") {
			return fmt.Errorf("%s refused: %s", st.what, reply)
		}
	}
	cmd("t4", "LOGOUT")
	return nil
}

func smtpSignIn(ctx context.Context, host, email, password string) error {
	c, err := dialFunnel(ctx, host, config.PublicSubmissionPort)
	if err != nil {
		return err
	}
	sc, err := smtp.NewClient(c, host)
	if err != nil {
		c.Close()
		return fmt.Errorf("no SMTP greeting: %w", err)
	}
	defer sc.Close()
	if err := sc.Hello("mailer-sign-in-test"); err != nil {
		return fmt.Errorf("EHLO refused: %w", err)
	}
	if ok, _ := sc.Extension("AUTH"); !ok {
		return errors.New("the server doesn't offer sign-in on this port")
	}
	if err := sc.Auth(smtp.PlainAuth("", email, password, host)); err != nil {
		return fmt.Errorf("sign-in refused: %w", err)
	}
	sc.Quit()
	return nil
}
