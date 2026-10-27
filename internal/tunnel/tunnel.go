// Package tunnel runs cloudflared, Cloudflare's tunnel connector, as a child
// process. If cloudflared isn't installed it downloads the official release
// for this OS/architecture from GitHub and verifies its SHA-256 checksum.
package tunnel

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"mailer/internal/console"
	"mailer/internal/users"
)

type Runner struct {
	BinDir string // where a downloaded cloudflared is kept
	Log    *log.Logger

	mu       sync.Mutex
	running  bool
	binPath  string
	lastErr  string
	lastLine string
	started  time.Time
}

type Status struct {
	Running  bool      `json:"running"`
	Binary   string    `json:"binary"`
	Since    time.Time `json:"since"`
	LastErr  string    `json:"last_error,omitempty"`
	LastLine string    `json:"last_line,omitempty"`
}

func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return Status{Running: r.running, Binary: r.binPath, Since: r.started, LastErr: r.lastErr, LastLine: r.lastLine}
}

func (r *Runner) set(fn func()) {
	r.mu.Lock()
	fn()
	r.mu.Unlock()
}

// Run keeps cloudflared connected while token() returns a non-empty token.
// It re-checks for a token every few seconds, so the tunnel starts as soon
// as Cloudflare setup finishes.
func (r *Runner) Run(ctx context.Context, token func() string) {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		tok := token()
		if tok == "" {
			sleep(ctx, 5*time.Second)
			continue
		}
		bin, err := r.ensureBinary(ctx)
		if err != nil {
			r.set(func() { r.lastErr = err.Error() })
			r.Log.Printf("tunnel: %v", err)
			sleep(ctx, time.Minute)
			continue
		}
		start := time.Now()
		err = r.runOnce(ctx, bin, tok)
		if ctx.Err() != nil {
			return
		}
		if time.Since(start) > 5*time.Minute {
			backoff = 5 * time.Second
		}
		msg := "cloudflared exited"
		if err != nil {
			msg += ": " + err.Error()
		}
		r.set(func() { r.lastErr = msg })
		r.Log.Printf("tunnel: %s; restarting in %s", msg, backoff)
		sleep(ctx, backoff)
		backoff = min(backoff*2, 2*time.Minute)
	}
}

func (r *Runner) runOnce(ctx context.Context, bin, token string) error {
	cmd := console.Hide(exec.CommandContext(ctx, bin, "tunnel", "--no-autoupdate", "run"))
	// Pass the token via the environment so it doesn't show in process lists.
	cmd.Env = append(os.Environ(), "TUNNEL_TOKEN="+token)
	out, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	cmd.Stdout = cmd.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	r.set(func() { r.running, r.started, r.lastErr = true, time.Now(), "" })
	r.Log.Printf("tunnel: cloudflared started (pid %d)", cmd.Process.Pid)
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		line := sc.Text()
		r.set(func() { r.lastLine = line })
		if strings.Contains(line, " ERR ") {
			r.Log.Printf("cloudflared: %s", line)
		}
	}
	err = cmd.Wait()
	r.set(func() { r.running = false })
	return err
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "cloudflared.exe"
	}
	return "cloudflared"
}

// ensureBinary finds cloudflared on PATH or in BinDir, downloading it if needed.
func (r *Runner) ensureBinary(ctx context.Context) (string, error) {
	if p, err := exec.LookPath("cloudflared"); err == nil {
		r.set(func() { r.binPath = p })
		return p, nil
	}
	local := filepath.Join(r.BinDir, exeName())
	if _, err := os.Stat(local); err == nil {
		r.set(func() { r.binPath = local })
		return local, nil
	}
	r.Log.Printf("tunnel: cloudflared not found; downloading the official release")
	if err := download(ctx, local); err != nil {
		return "", fmt.Errorf("downloading cloudflared: %w", err)
	}
	r.set(func() { r.binPath = local })
	return local, nil
}

func assetName() (string, error) {
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "linux/amd64", "linux/arm64", "linux/386":
		return "cloudflared-linux-" + runtime.GOARCH, nil
	case "linux/arm":
		return "cloudflared-linux-arm", nil
	case "darwin/amd64", "darwin/arm64":
		return "cloudflared-darwin-" + runtime.GOARCH + ".tgz", nil
	case "windows/amd64", "windows/arm64":
		return "cloudflared-windows-amd64.exe", nil // runs under emulation on arm64
	case "windows/386":
		return "cloudflared-windows-386.exe", nil
	}
	return "", fmt.Errorf("no cloudflared build for %s/%s; install it manually", runtime.GOOS, runtime.GOARCH)
}

type release struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
	Assets  []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"` // "sha256:<hex>"
	} `json:"assets"`
}

func download(ctx context.Context, dest string) error {
	asset, err := assetName()
	if err != nil {
		return err
	}
	hc := &http.Client{Timeout: 5 * time.Minute}
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/repos/cloudflare/cloudflared/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	var rel release
	err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel)
	resp.Body.Close()
	if err != nil {
		return fmt.Errorf("reading release info: %w", err)
	}

	var url, want string
	for _, a := range rel.Assets {
		if a.Name == asset {
			url = a.URL
			want = strings.TrimPrefix(a.Digest, "sha256:")
		}
	}
	if url == "" {
		return fmt.Errorf("release %s has no %s", rel.TagName, asset)
	}
	if want == "" { // fall back to the checksum list in the release notes
		re := regexp.MustCompile(regexp.QuoteMeta(asset) + `:\s*([0-9a-f]{64})`)
		if m := re.FindStringSubmatch(rel.Body); m != nil {
			want = m[1]
		}
	}
	if want == "" {
		return errors.New("no published checksum to verify against; install cloudflared manually")
	}

	req, _ = http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err = hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 200<<20))
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != want {
		return fmt.Errorf("checksum mismatch for %s (got %s, want %s)", asset, got, want)
	}

	if strings.HasSuffix(asset, ".tgz") {
		if data, err = extractTgz(data, "cloudflared"); err != nil {
			return err
		}
	}
	if err := users.WriteFileAtomic(dest, data, 0o755); err != nil {
		return err
	}
	return nil
}

func extractTgz(data []byte, name string) ([]byte, error) {
	gz, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("%s not found in archive", name)
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(h.Name) == name && h.Typeflag == tar.TypeReg {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}
