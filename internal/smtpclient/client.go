// Package smtpclient delivers mail directly to recipients' MX servers.
package smtpclient

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Resolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// Result is the outcome for one recipient.
type Result struct {
	Rcpt      string
	Err       error // nil on success
	Permanent bool  // true if retrying will not help
}

type Client struct {
	Hostname func() string // our EHLO name
	Resolver Resolver
	Dial     func(ctx context.Context, network, addr string) (net.Conn, error)
}

// Deliver sends data to rcpts, all of which must be in domain.
func (c *Client) Deliver(ctx context.Context, domain, from string, rcpts []string, data []byte) []Result {
	all := func(err error, perm bool) []Result {
		out := make([]Result, len(rcpts))
		for i, r := range rcpts {
			out[i] = Result{Rcpt: r, Err: err, Permanent: perm}
		}
		return out
	}

	hosts, err := c.mxHosts(ctx, domain)
	if err != nil {
		var pe *permError
		return all(err, errors.As(err, &pe))
	}

	var lastErr error = errors.New("no reachable mail servers")
	for _, host := range hosts {
		addrs, err := c.Resolver.LookupIPAddr(ctx, host)
		if err != nil {
			lastErr = fmt.Errorf("resolving %s: %w", host, err)
			continue
		}
		for _, a := range addrs {
			addr := net.JoinHostPort(a.IP.String(), "25")
			res, err := c.try(ctx, host, addr, from, rcpts, data, false)
			if errors.Is(err, errTLSVerify) {
				// Opportunistic TLS: most MX certificates are valid, but
				// unencrypted-or-unverified is still the norm for SMTP.
				res, err = c.try(ctx, host, addr, from, rcpts, data, true)
			}
			if err == nil {
				return res
			}
			lastErr = err
		}
	}
	return all(lastErr, false)
}

type permError struct{ msg string }

func (e *permError) Error() string { return e.msg }

func (c *Client) mxHosts(ctx context.Context, domain string) ([]string, error) {
	mxs, err := c.Resolver.LookupMX(ctx, domain)
	if err != nil {
		var de *net.DNSError
		if errors.As(err, &de) && de.IsNotFound {
			// No MX: fall back to the domain itself (implicit MX), if it resolves.
			if _, err := c.Resolver.LookupIPAddr(ctx, domain); err != nil {
				return nil, &permError{"domain " + domain + " has no mail server"}
			}
			return []string{domain}, nil
		}
		return nil, fmt.Errorf("MX lookup for %s: %w", domain, err)
	}
	if len(mxs) == 1 && (mxs[0].Host == "." || mxs[0].Host == "") {
		return nil, &permError{"domain " + domain + " does not accept mail (null MX)"}
	}
	sort.SliceStable(mxs, func(i, j int) bool { return mxs[i].Pref < mxs[j].Pref })
	hosts := make([]string, 0, len(mxs))
	for _, mx := range mxs {
		hosts = append(hosts, strings.TrimSuffix(mx.Host, "."))
	}
	return hosts, nil
}

var errTLSVerify = errors.New("TLS certificate verification failed")

type reply struct {
	code int
	msg  string
}

func (r reply) err() error { return fmt.Errorf("%d %s", r.code, r.msg) }

type conn struct {
	c net.Conn
	r *bufio.Reader
	w *bufio.Writer
}

func (cn *conn) read() (reply, error) {
	var msgs []string
	for {
		cn.c.SetReadDeadline(time.Now().Add(5 * time.Minute))
		line, err := cn.r.ReadString('\n')
		if err != nil {
			return reply{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if len(line) < 3 {
			return reply{}, fmt.Errorf("malformed reply %q", line)
		}
		code, err := strconv.Atoi(line[:3])
		if err != nil {
			return reply{}, fmt.Errorf("malformed reply %q", line)
		}
		if len(line) > 4 {
			msgs = append(msgs, line[4:])
		}
		if len(line) == 3 || line[3] == ' ' {
			return reply{code, strings.Join(msgs, "\n")}, nil
		}
	}
}

func (cn *conn) cmd(format string, a ...any) (reply, error) {
	fmt.Fprintf(cn.w, format+"\r\n", a...)
	if err := cn.w.Flush(); err != nil {
		return reply{}, err
	}
	return cn.read()
}

// try runs one SMTP session. A non-nil error means "try the next server";
// otherwise the per-recipient results are final for this attempt.
func (c *Client) try(ctx context.Context, host, addr, from string, rcpts []string, data []byte, insecure bool) ([]Result, error) {
	dial := c.Dial
	if dial == nil {
		d := &net.Dialer{Timeout: 30 * time.Second}
		dial = d.DialContext
	}
	nc, err := dial(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer nc.Close()
	cn := &conn{c: nc, r: bufio.NewReader(nc), w: bufio.NewWriter(nc)}

	rep, err := cn.read()
	if err != nil {
		return nil, err
	}
	if rep.code/100 != 2 {
		return nil, fmt.Errorf("greeting from %s: %w", host, rep.err())
	}
	ext, err := c.ehlo(cn)
	if err != nil {
		return nil, err
	}
	if _, ok := ext["STARTTLS"]; ok {
		rep, err := cn.cmd("STARTTLS")
		if err != nil {
			return nil, err
		}
		if rep.code == 220 {
			tc := tls.Client(nc, &tls.Config{ServerName: host, InsecureSkipVerify: insecure, MinVersion: tls.VersionTLS12})
			tc.SetDeadline(time.Now().Add(30 * time.Second))
			if err := tc.Handshake(); err != nil {
				var ve *tls.CertificateVerificationError
				if errors.As(err, &ve) && !insecure {
					return nil, errTLSVerify
				}
				return nil, err
			}
			tc.SetDeadline(time.Time{})
			cn = &conn{c: tc, r: bufio.NewReader(tc), w: bufio.NewWriter(tc)}
			if ext, err = c.ehlo(cn); err != nil {
				return nil, err
			}
		}
	}

	results := make([]Result, len(rcpts))
	finish := func(err error, perm bool, idx []int) {
		for _, i := range idx {
			results[i] = Result{Rcpt: rcpts[i], Err: err, Permanent: perm}
		}
	}
	allIdx := make([]int, len(rcpts))
	for i := range rcpts {
		allIdx[i] = i
	}

	params := ""
	if _, ok := ext["SIZE"]; ok {
		params += fmt.Sprintf(" SIZE=%d", len(data))
	}
	if _, ok := ext["8BITMIME"]; ok && has8bit(data) {
		params += " BODY=8BITMIME"
	}
	rep, err = cn.cmd("MAIL FROM:<%s>%s", from, params)
	if err != nil {
		return nil, err
	}
	if rep.code/100 != 2 {
		finish(rep.err(), rep.code/100 == 5, allIdx)
		cn.cmd("QUIT")
		return results, nil
	}
	var accepted []int
	for i, r := range rcpts {
		rep, err := cn.cmd("RCPT TO:<%s>", r)
		if err != nil {
			return nil, err
		}
		if rep.code/100 == 2 {
			accepted = append(accepted, i)
		} else {
			finish(rep.err(), rep.code/100 == 5, []int{i})
		}
	}
	if len(accepted) == 0 {
		cn.cmd("QUIT")
		return results, nil
	}
	rep, err = cn.cmd("DATA")
	if err != nil {
		return nil, err
	}
	if rep.code != 354 {
		finish(rep.err(), rep.code/100 == 5, accepted)
		cn.cmd("QUIT")
		return results, nil
	}
	cn.c.SetWriteDeadline(time.Now().Add(10 * time.Minute))
	writeDotStuffed(cn.w, data)
	cn.w.WriteString(".\r\n")
	if err := cn.w.Flush(); err != nil {
		return nil, err
	}
	rep, err = cn.read()
	if err != nil {
		return nil, err
	}
	if rep.code/100 == 2 {
		finish(nil, false, accepted)
	} else {
		finish(rep.err(), rep.code/100 == 5, accepted)
	}
	cn.cmd("QUIT")
	return results, nil
}

func (c *Client) ehlo(cn *conn) (map[string]string, error) {
	rep, err := cn.cmd("EHLO %s", c.Hostname())
	if err != nil {
		return nil, err
	}
	ext := map[string]string{}
	if rep.code != 250 {
		rep, err = cn.cmd("HELO %s", c.Hostname())
		if err != nil {
			return nil, err
		}
		if rep.code != 250 {
			return nil, rep.err()
		}
		return ext, nil
	}
	lines := strings.Split(rep.msg, "\n")
	for _, l := range lines[1:] {
		k, v, _ := strings.Cut(l, " ")
		ext[strings.ToUpper(k)] = v
	}
	return ext, nil
}

func writeDotStuffed(w *bufio.Writer, data []byte) {
	for len(data) > 0 {
		i := bytes.Index(data, []byte("\r\n"))
		var line []byte
		if i < 0 {
			line, data = data, nil
		} else {
			line, data = data[:i], data[i+2:]
		}
		if len(line) > 0 && line[0] == '.' {
			w.WriteByte('.')
		}
		w.Write(line)
		w.WriteString("\r\n")
	}
}

func has8bit(b []byte) bool {
	for _, c := range b {
		if c >= 0x80 {
			return true
		}
	}
	return false
}
