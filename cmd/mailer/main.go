// Command mailer is a self-contained mail server for your own domain.
// Run it, open the admin page it prints, and set everything up there.
package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"mailer/internal/admin"
	"mailer/internal/cloudflare"
	"mailer/internal/config"
	"mailer/internal/imap"
	"mailer/internal/logbuf"
	"mailer/internal/mailauth"
	"mailer/internal/mta"
	"mailer/internal/queue"
	"mailer/internal/smtp"
	"mailer/internal/smtpclient"
	"mailer/internal/store"
	"mailer/internal/tunnel"
	"mailer/internal/users"
)

const maxClientMessage = 25 << 20

func defaultDataDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "mailer-data")
	}
	return "mailer-data"
}

func main() {
	dataDir := flag.String("data", defaultDataDir(), "directory for mail, accounts, keys and settings")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: mailer [-data DIR] [install | uninstall]\n\n"+
			"  (no command)  run the server; the admin page opens at http://127.0.0.1:8080\n"+
			"  install       start mailer automatically at boot (run as admin/root)\n"+
			"  uninstall     remove the boot service\n")
	}
	flag.Parse()
	abs, err := filepath.Abs(*dataDir)
	fatal(err)

	switch flag.Arg(0) {
	case "":
		fatal(serve(abs))
	case "install":
		fatal(install(abs))
	case "uninstall":
		fatal(uninstall())
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func serve(dataDir string) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	logs := logbuf.New(1000)
	logger := log.New(io.MultiWriter(os.Stderr, logs), "", log.LstdFlags)

	cfgm, err := config.Open(dataDir)
	if err != nil {
		return err
	}
	cfg := cfgm.Get()
	db, err := users.Open(filepath.Join(dataDir, "users.json"))
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dkim := &dkimKeys{dir: filepath.Join(dataDir, "dkim"), cfg: cfgm, keys: map[string]*mailauth.Signer{}}
	st := store.New(filepath.Join(dataDir, "mail"))
	limiter := users.NewLimiter()
	resolver := net.DefaultResolver
	hostname := func() string { return cfgm.Get().Hostname }

	m := &mta.MTA{Cfg: cfgm, Users: db, Store: st, Resolver: resolver, DKIM: dkim.signer, Log: logger}
	q, err := queue.New(filepath.Join(dataDir, "queue"),
		&smtpclient.Client{Hostname: hostname, Resolver: resolver}, m.Bounce, logger)
	if err != nil {
		return err
	}
	m.Queue = q

	setup := &cloudflare.Setup{Cfg: cfgm, DKIMRecord: dkim.record, Log: logger}
	runner := &tunnel.Runner{BinDir: filepath.Join(dataDir, "bin"), Log: logger}
	adm := &admin.Server{Cfg: cfgm, Users: db, Store: st, Queue: q, MTA: m, Setup: setup,
		Tunnel: runner, Logs: logs, Log: logger}

	sub := &smtp.Server{Hostname: hostname, Mode: smtp.ModeSubmission, Users: db, Limiter: limiter,
		MaxSize: maxClientMessage, Backend: mta.Submission{MTA: m}, Log: logger}
	im := &imap.Server{Hostname: hostname, Users: db, Store: st, Limiter: limiter,
		MaxMessage: maxClientMessage, Log: logger}

	// Everything listens on loopback only. The internet reaches us through
	// the Cloudflare Tunnel (inbound mail) and Tailscale Funnel (clients).
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

	adminL, err := listen("admin UI", loop(cfg.Ports.Admin))
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

	go (&http.Server{Handler: adm.Handler(), ReadHeaderTimeout: 10 * time.Second}).Serve(adminL)
	go (&http.Server{Handler: m.InboundHandler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 5 * time.Minute}).Serve(inboundL)
	go im.Serve(imapL, true)
	go sub.Serve(subL, true)
	go q.Run(ctx)
	go runner.Run(ctx, func() string { return cfgm.Get().Cloudflare.TunnelToken })
	go adm.RunChecks(ctx)
	go syncDNS(ctx, setup, cfgm, logger)

	url := fmt.Sprintf("http://127.0.0.1:%d", cfg.Ports.Admin)
	logger.Printf("mailer running. Admin page: %s", url)
	if cfg.Cloudflare.ZoneID == "" {
		openBrowser(url)
	}
	<-ctx.Done()
	logger.Printf("shutting down")
	return nil
}

// syncDNS keeps the SPF record and HELO hostname pointed at our current
// public IP, which can change on a home connection.
func syncDNS(ctx context.Context, setup *cloudflare.Setup, cfgm *config.Manager, logger *log.Logger) {
	for {
		if cfgm.Get().Cloudflare.ZoneID != "" {
			c, cancel := context.WithTimeout(ctx, time.Minute)
			if _, err := setup.SyncDNS(c); err != nil {
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

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return // headless server
		}
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

// ---- boot service ----

const serviceName = "mailer"

func install(dataDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	switch runtime.GOOS {
	case "linux":
		unit := fmt.Sprintf(`[Unit]
Description=Mailer mail server
After=network-online.target tailscaled.service
Wants=network-online.target

[Service]
ExecStart=%q -data %q
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`, exe, dataDir)
		if err := os.WriteFile("/etc/systemd/system/"+serviceName+".service", []byte(unit), 0o644); err != nil {
			return fmt.Errorf("%w (run with sudo)", err)
		}
		if err := runCmd("systemctl", "daemon-reload"); err != nil {
			return err
		}
		return runCmd("systemctl", "enable", "--now", serviceName)
	case "darwin":
		plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>com.mailer</string>
  <key>ProgramArguments</key><array><string>%s</string><string>-data</string><string>%s</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardErrorPath</key><string>%s</string>
</dict></plist>
`, exe, dataDir, filepath.Join(dataDir, "mailer.log"))
		path := "/Library/LaunchDaemons/com.mailer.plist"
		if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
			return fmt.Errorf("%w (run with sudo)", err)
		}
		return runCmd("launchctl", "load", "-w", path)
	case "windows":
		tr := fmt.Sprintf(`"%s" -data "%s"`, exe, dataDir)
		if err := runCmd("schtasks", "/Create", "/TN", "Mailer", "/TR", tr, "/SC", "ONSTART", "/RU", "SYSTEM", "/RL", "HIGHEST", "/F"); err != nil {
			return fmt.Errorf("%w (run from an Administrator terminal)", err)
		}
		return runCmd("schtasks", "/Run", "/TN", "Mailer")
	}
	return errors.New("install isn't supported on " + runtime.GOOS)
}

func uninstall() error {
	switch runtime.GOOS {
	case "linux":
		runCmd("systemctl", "disable", "--now", serviceName)
		return os.Remove("/etc/systemd/system/" + serviceName + ".service")
	case "darwin":
		path := "/Library/LaunchDaemons/com.mailer.plist"
		runCmd("launchctl", "unload", "-w", path)
		return os.Remove(path)
	case "windows":
		runCmd("schtasks", "/End", "/TN", "Mailer")
		return runCmd("schtasks", "/Delete", "/TN", "Mailer", "/F")
	}
	return errors.New("uninstall isn't supported on " + runtime.GOOS)
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}
