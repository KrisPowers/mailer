// Package proxyproto reads an optional PROXY protocol (v1 or v2) header so
// connections forwarded by Tailscale Funnel keep the real client address.
package proxyproto

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strings"
	"time"
)

var v2sig = []byte("\r\n\r\n\x00\r\nQUIT\n")

// Conn is a net.Conn whose reads continue after the PROXY header.
type Conn struct {
	net.Conn
	r *bufio.Reader
}

func (c *Conn) Read(p []byte) (int, error) { return c.r.Read(p) }

// Read waits up to wait for a PROXY header. If none arrives (a local client
// connecting directly), the connection is returned unchanged with its own
// remote address. Mail protocols are server-speaks-first, so a client that
// isn't a proxy sends nothing until we greet it.
func Read(c net.Conn, wait time.Duration) (net.Conn, net.IP, error) {
	ip := addrIP(c.RemoteAddr())
	br := bufio.NewReaderSize(c, 512)
	c.SetReadDeadline(time.Now().Add(wait))
	defer c.SetReadDeadline(time.Time{})

	head, err := br.Peek(12)
	if err != nil && len(head) == 0 {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return &Conn{c, br}, ip, nil
		}
		return nil, nil, err
	}
	switch {
	case bytes.Equal(head, v2sig):
		src, err := readV2(br)
		if err != nil {
			return nil, nil, err
		}
		if src != nil {
			ip = src
		}
	case bytes.HasPrefix(head, []byte("PROXY ")):
		line, err := br.ReadString('\n')
		if err != nil || len(line) > 107 {
			return nil, nil, errors.New("proxyproto: bad v1 header")
		}
		f := strings.Fields(line)
		if len(f) >= 3 && f[1] != "UNKNOWN" {
			if p := net.ParseIP(f[2]); p != nil {
				ip = p
			}
		}
	}
	return &Conn{c, br}, ip, nil
}

func readV2(br *bufio.Reader) (net.IP, error) {
	hdr := make([]byte, 16)
	if _, err := io.ReadFull(br, hdr); err != nil {
		return nil, err
	}
	verCmd, fam := hdr[12], hdr[13]
	n := int(binary.BigEndian.Uint16(hdr[14:16]))
	if verCmd>>4 != 2 || n > 4096 {
		return nil, errors.New("proxyproto: bad v2 header")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, err
	}
	if verCmd&0x0f != 1 { // LOCAL: keep the socket address
		return nil, nil
	}
	switch fam >> 4 {
	case 1: // IPv4
		if n >= 12 {
			return net.IP(body[0:4]), nil
		}
	case 2: // IPv6
		if n >= 36 {
			return net.IP(body[0:16]), nil
		}
	}
	return nil, nil
}

func addrIP(a net.Addr) net.IP {
	if ta, ok := a.(*net.TCPAddr); ok {
		return ta.IP
	}
	return nil
}
