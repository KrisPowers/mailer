// Command mailer is a self-contained mail server for your own domains. It
// runs in a container beside cloudflared and Tailscale, and is set up and
// managed from its web dashboard.
package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"mailer/internal/admin"
	"mailer/internal/audit"
	"mailer/internal/auth"
	"mailer/internal/cloudflare"
	"mailer/internal/config"
	"mailer/internal/imap"
	"mailer/internal/logbuf"
	"mailer/internal/mailauth"
	"mailer/internal/maillog"
	"mailer/internal/mta"
	"mailer/internal/queue"
	"mailer/internal/smtp"
	"mailer/internal/store"
	"mailer/internal/tailscale"
	"mailer/internal/tunnel"
	"mailer/internal/users"
)

const maxClientMessage = 25 << 20

// env returns the environment variable key, or def when it's unset.
func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	dataDir := flag.String("data", env("MAILER_DATA", "mailer-data"), "directory for mail, accounts, keys and settings")
	listen := flag.String("listen", env("MAILER_LISTEN", "127.0.0.1"),
		"address the dashboard and the Cloudflare sign-in callback listen on (0.0.0.0 in the container)")
	flag.Parse()
	abs, err := filepath.Abs(*dataDir)
	if err == nil {
		err = serve(abs, *listen)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func serve(dataDir, listenHost string) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	logs := logbuf.New(1000)
	// mailer.log keeps the log across restarts; the previous one is kept
	// as mailer.log.old once it passes 5 MB.
	logPath := filepath.Join(dataDir, "mailer.log")
	if st, err := os.Stat(logPath); err == nil && st.Size() > 5<<20 {
		os.Rename(logPath, logPath+".old")
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	// docker logs shows stderr.
	logger := log.New(io.MultiWriter(logs, logFile, os.Stderr), "", log.LstdFlags)

	cfgm, err := config.Open(dataDir)
	if err != nil {
		return err
	}
	cfg := cfgm.Get()
	db, err := users.Open(filepath.Join(dataDir, "users.json"))
	if err != nil {
		return err
	}
	dashboardAuth, err := auth.Open(filepath.Join(dataDir, "auth.json"))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dkim := &dkimKeys{dir: filepath.Join(dataDir, "dkim"), cfg: cfgm, keys: map[string]*mailauth.Signer{}}
	st := store.New(filepath.Join(dataDir, "mail"))
	limiter := users.NewLimiter()
	signIns := audit.Open(filepath.Join(dataDir, "signins.log"))
	resolver := net.DefaultResolver
	hostname := func() string { return cfgm.Get().Hostname }

	mailLog := maillog.Open(filepath.Join(dataDir, "mail.log"))
	m := &mta.MTA{Cfg: cfgm, Users: db, Store: st, Resolver: resolver, DKIM: dkim.signer, Log: logger, Mail: mailLog}
	setup := &cloudflare.Setup{Cfg: cfgm, DKIMRecord: dkim.record, Log: logger, CallbackHost: listenHost}
	q, err := queue.New(filepath.Join(dataDir, "queue"), &cloudflare.Relay{Setup: setup}, m.Bounce, logger)
	if err != nil {
		return err
	}
	q.Mail = mailLog
	m.Queue = q

	runner := &tunnel.Runner{BinDir: filepath.Join(dataDir, "bin"), Log: logger}
	adm := &admin.Server{Cfg: cfgm, Users: db, Store: st, Queue: q, MTA: m, Setup: setup,
		Tunnel: runner, Logs: logs, Log: logger, Audit: signIns, Limiter: limiter, Auth: dashboardAuth}

	sub := &smtp.Server{Hostname: hostname, Mode: smtp.ModeSubmission, Users: db, Limiter: limiter,
		MaxSize: maxClientMessage, Backend: mta.Submission{MTA: m}, Log: logger, Audit: signIns}
	im := &imap.Server{Hostname: hostname, Users: db, Store: st, Limiter: limiter,
		MaxMessage: maxClientMessage, Log: logger, Audit: signIns}
	// Tracing every mail app command, and overriding the IMAP extensions
	// advertised, are switched in the dashboard for debugging sign-in and
	// sync problems, and apply to the next connection.
	sub.Trace = func() bool { return cfgm.Get().Trace }
	im.Trace = sub.Trace
	im.Caps = func() string { return cfgm.Get().IMAPCaps }

	// Mail ports listen on loopback only: the internet reaches them through
	// the Cloudflare Tunnel (inbound mail) and Tailscale Funnel (clients),
	// both running in this container. The dashboard listens on listenHost,
	// which the container publishes to the host's loopback only.
	loop := func(port int) string { return fmt.Sprintf("127.0.0.1:%d", port) }
	var listeners []net.Listener
	listen := func(name, addr string) (net.Listener, error) {
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("%s on %s: %w (is another copy of mailer running?)", name, addr, err)
		}
		listeners = append(listeners, l)
		return l, nil
	}
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()

	adminL, err := listen("dashboard", net.JoinHostPort(listenHost, fmt.Sprint(cfg.Ports.Admin)))
	if err != nil {
		return err
	}
	inboundL, err := listen("inbound", loop(cfg.Ports.Inbound))
	if err != nil {
		return err
	}
	imapL, err := listen("IMAP", loop(cfg.Ports.IMAP))
	if err != nil {
		return err
	}
	subL, err := listen("submission", loop(cfg.Ports.Submission))
	if err != nil {
		return err
	}

	tunnelDone := make(chan struct{})
	go (&http.Server{Handler: adm.Handler(), ReadHeaderTimeout: 10 * time.Second}).Serve(adminL)
	go (&http.Server{Handler: m.InboundHandler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 5 * time.Minute}).Serve(inboundL)
	go im.Serve(imapL, true)
	go sub.Serve(subL, true)
	go q.Run(ctx)
	go func() {
		runner.Run(ctx, func() string { return cfgm.Get().Cloudflare.TunnelToken })
		close(tunnelDone)
	}()
	// Once set up, Tailscale signs in (or reconnects) under the name and key
	// saved in the dashboard; until then it waits.
	if !dashboardAuth.NeedsSetup() {
		tailscale.Up(ctx, cfg.Tailscale.Hostname, cfg.Tailscale.AuthKey, logger.Printf)
	}
	go adm.RunChecks(ctx)
	go syncDNS(ctx, setup, cfgm, logger)
	logger.Printf("mailer running; dashboard on port %d", cfg.Ports.Admin)
	<-ctx.Done()

	logger.Printf("shutting down")
	stop()
	// Wait for the tunnel connector to exit so it doesn't outlive us.
	select {
	case <-tunnelDone:
	case <-time.After(5 * time.Second):
	}
	return nil
}

// syncDNS connects every domain on the Cloudflare account and keeps each
// one's SPF record and mail app settings names in place.
func syncDNS(ctx context.Context, setup *cloudflare.Setup, cfgm *config.Manager, logger *log.Logger) {
	for {
		if cfg := cfgm.Get(); cfg.Connected() {
			c, cancel := context.WithTimeout(ctx, 5*time.Minute)
			if err := setup.SyncZones(c); err != nil {
				logger.Printf("cloudflare: listing domains: %v", err)
			}
			if err := setup.SyncDNS(c); err != nil {
				logger.Printf("dns: %v", err)
			}
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Minute):
		}
	}
}

// dkimKeys loads or creates one RSA key per domain.
type dkimKeys struct {
	dir  string
	cfg  *config.Manager
	mu   sync.Mutex
	keys map[string]*mailauth.Signer
}

func (d *dkimKeys) signer(domain string) *mailauth.Signer {
	cfg := d.cfg.Get()
	if !cfg.IsLocalDomain(domain) {
		return nil
	}
	s, err := d.load(domain, cfg.DKIMSelector)
	if err != nil {
		return nil
	}
	return s
}

func (d *dkimKeys) record(domain string) (string, error) {
	s, err := d.load(domain, d.cfg.Get().DKIMSelector)
	if err != nil {
		return "", err
	}
	return s.DNSRecord(), nil
}

func (d *dkimKeys) load(domain, selector string) (*mailauth.Signer, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s, ok := d.keys[domain]; ok {
		return s, nil
	}
	path := filepath.Join(d.dir, domain+".pem")
	var key *rsa.PrivateKey
	if b, err := os.ReadFile(path); err == nil {
		blk, _ := pem.Decode(b)
		if blk == nil {
			return nil, fmt.Errorf("%s: no PEM data", path)
		}
		k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
		if err != nil {
			return nil, err
		}
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("%s: not an RSA key", path)
		}
		key = rk
	} else {
		if key, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
			return nil, err
		}
		der, _ := x509.MarshalPKCS8PrivateKey(key)
		if err := users.WriteFileAtomic(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
			return nil, err
		}
	}
	s := &mailauth.Signer{Domain: domain, Selector: selector, Key: key}
	d.keys[domain] = s
	return s, nil
}
