// Package mta routes mail: it receives inbound messages (relayed by the
// Cloudflare Email Worker), is the smtp.Backend for client submission,
// delivers to local mailboxes, and hands remote mail to the outbound queue.
package mta

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"net/mail"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"mailer/internal/config"
	"mailer/internal/mailauth"
	"mailer/internal/maillog"
	"mailer/internal/message"
	"mailer/internal/queue"
	"mailer/internal/smtp"
	"mailer/internal/store"
	"mailer/internal/users"
)

type MTA struct {
	Cfg      *config.Manager
	Users    *users.DB
	Store    *store.Store
	Queue    *queue.Queue
	Resolver mailauth.Resolver
	// DKIM returns the signer for a domain, or nil.
	DKIM func(domain string) *mailauth.Signer
	Log  *log.Logger
	Mail *maillog.Log // what happened to each message, for the dashboard

	lastInbound atomic.Int64 // unix seconds
}

// LastInbound reports when mail was last received.
func (m *MTA) LastInbound() time.Time {
	if t := m.lastInbound.Load(); t != 0 {
		return time.Unix(t, 0)
	}
	return time.Time{}
}

func splitAddr(addr string) (local, domain string) {
	i := strings.LastIndexByte(addr, '@')
	if i < 0 {
		return addr, ""
	}
	return addr[:i], strings.ToLower(addr[i+1:])
}

// Resolve maps a recipient address to a local user, following plus-addressing
// (user+tag@), aliases and catch-alls.
func (m *MTA) Resolve(addr string) (string, bool) {
	cfg := m.Cfg.Get()
	addr = strings.ToLower(strings.TrimSpace(addr))
	local, domain := splitAddr(addr)
	if !cfg.IsLocalDomain(domain) {
		return "", false
	}
	candidates := []string{addr}
	if i := strings.IndexByte(local, '+'); i > 0 {
		candidates = append(candidates, local[:i]+"@"+domain)
	}
	for _, c := range candidates {
		if m.Users.Exists(c) {
			return c, true
		}
		if t, ok := cfg.Aliases[c]; ok && m.Users.Exists(t) {
			return t, true
		}
	}
	if t, ok := cfg.Aliases["@"+domain]; ok && m.Users.Exists(t) {
		return t, true
	}
	// RFC 5321 requires postmaster to work; route it to the first user.
	if base := strings.SplitN(local, "+", 2)[0]; base == "postmaster" || base == "abuse" {
		if list := m.Users.List(); len(list) > 0 {
			return list[0], true
		}
	}
	return "", false
}

// mayUse reports whether user may send as addr.
func (m *MTA) mayUse(user, addr string) bool {
	u, ok := m.Resolve(addr)
	return ok && u == user
}

func (m *MTA) deliverLocal(user string, junk bool, data []byte) error {
	acct, err := m.Store.Account(user)
	if err != nil {
		return err
	}
	box := "INBOX"
	if junk {
		if j := acct.FindSpecialUse(`\Junk`); j != "" {
			box = j
		}
	}
	_, _, err = acct.Append(box, data, nil, time.Now())
	return err
}

func tempErr(msg string) error { return &smtp.Error{Code: 451, Enhanced: "4.3.0", Msg: msg} }

// ---- inbound ----

// CheckRecipient validates an inbound recipient.
func (m *MTA) CheckRecipient(rcpt string) error {
	cfg := m.Cfg.Get()
	_, domain := splitAddr(rcpt)
	if !cfg.IsLocalDomain(domain) {
		return &smtp.Error{Code: 554, Enhanced: "5.7.1", Msg: "relay access denied"}
	}
	user, ok := m.Resolve(rcpt)
	if !ok {
		return &smtp.Error{Code: 550, Enhanced: "5.1.1", Msg: "no such user"}
	}
	if m.Users.Locks(user).Receive {
		return &smtp.Error{Code: 550, Enhanced: "5.2.1", Msg: "mailbox is not accepting mail"}
	}
	return nil
}

// InboundMessage is a message received from the outside world.
type InboundMessage struct {
	RemoteIP net.IP // connecting server, if known
	Helo     string
	From     string // envelope sender
	To       []string
	Data     []byte
	Via      string // e.g. "Cloudflare Email Routing"
	Junk     bool   // set by Receive when it filed the message in Junk
}

// spfFallback is a pre-computed SPF result, used when RemoteIP is unknown.
func (m *MTA) Receive(msg *InboundMessage, spfFallback mailauth.SPFResult) error {
	cfg := m.Cfg.Get()
	for _, r := range msg.To {
		if err := m.CheckRecipient(r); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	hdrBytes, _ := message.SplitHeader(msg.Data)
	header := message.ParseHeader(hdrBytes)

	spf, spfDomain := spfFallback, ""
	if at := strings.LastIndexByte(msg.From, '@'); at >= 0 {
		spfDomain = strings.ToLower(msg.From[at+1:])
	}
	if msg.RemoteIP != nil {
		spf, spfDomain = mailauth.CheckSPF(ctx, m.Resolver, msg.RemoteIP, msg.From, msg.Helo)
	}
	if spf == "" {
		spf = mailauth.SPFNone
	}
	dkims := mailauth.VerifyDKIM(ctx, m.Resolver, msg.Data)

	fromDomain := ""
	if froms, err := mail.ParseAddressList(header.Get("From")); err == nil && len(froms) == 1 {
		_, fromDomain = splitAddr(froms[0].Address)
	}
	dmarc := mailauth.CheckDMARC(ctx, m.Resolver, fromDomain, spf, spfDomain, dkims)

	junk := false
	var reasons []string
	switch {
	case fromDomain == "":
		junk = true
		reasons = append(reasons, "missing or invalid From")
	case dmarc.Result == "fail" && dmarc.Policy == "reject" && cfg.RejectDMARCFailures:
		m.Log.Printf("inbound: rejected from=%s header-from=%s: DMARC reject", msg.From, fromDomain)
		return &smtp.Error{Code: 550, Enhanced: "5.7.1", Msg: "rejected by sender's DMARC policy"}
	case dmarc.Result == "fail" && dmarc.Policy != "none":
		junk = true
		reasons = append(reasons, "DMARC "+dmarc.Policy)
	case spf == mailauth.SPFFail && dmarc.Result != "pass":
		junk = true
		reasons = append(reasons, "SPF fail")
	}
	// Mail claiming to be from our own domains must authenticate.
	if cfg.IsLocalDomain(fromDomain) && dmarc.Result != "pass" {
		junk = true
		reasons = append(reasons, "unauthenticated use of local domain")
	}

	var trace bytes.Buffer
	fmt.Fprintf(&trace, "Authentication-Results: %s;\r\n\tspf=%s smtp.mailfrom=%s", cfg.Hostname, spf, spfDomain)
	if len(dkims) == 0 {
		trace.WriteString(";\r\n\tdkim=none")
	}
	for _, d := range dkims {
		fmt.Fprintf(&trace, ";\r\n\tdkim=%s header.d=%s header.s=%s", d.Status, d.Domain, d.Selector)
	}
	fmt.Fprintf(&trace, ";\r\n\tdmarc=%s (p=%s) header.from=%s\r\n", dmarc.Result, dmarc.Policy, fromDomain)
	if junk {
		fmt.Fprintf(&trace, "X-Mailer-Junk: yes (%s)\r\n", strings.Join(reasons, ", "))
	}
	via := msg.Via
	if via == "" {
		via = "ESMTP"
	}
	ipText := "unknown"
	if msg.RemoteIP != nil {
		ipText = msg.RemoteIP.String()
	}
	fmt.Fprintf(&trace, "Received: from %s ([%s])\r\n\tby %s via %s;\r\n\t%s\r\n",
		sanitize(msg.Helo), ipText, cfg.Hostname, via, time.Now().Format(time.RFC1123Z))

	data := append(trace.Bytes(), stripOwnAuthResults(msg.Data, cfg.Hostname)...)

	delivered := map[string]bool{}
	for _, rcpt := range msg.To {
		user, ok := m.Resolve(rcpt)
		if !ok || delivered[user] {
			continue
		}
		if err := m.deliverLocal(user, junk, data); err != nil {
			m.Log.Printf("inbound: local delivery to %s failed: %v", user, err)
			return tempErr("local delivery failed")
		}
		delivered[user] = true
	}
	msg.Junk = junk
	m.lastInbound.Store(time.Now().Unix())
	m.Log.Printf("inbound: from=%s to=%v spf=%s dmarc=%s junk=%v size=%d",
		msg.From, msg.To, spf, dmarc.Result, junk, len(msg.Data))
	return nil
}

var (
	clientIPRe = regexp.MustCompile(`(?i)client-ip=([0-9a-f:.]+)`)
	heloRe     = regexp.MustCompile(`(?i)helo=([^\s;]+)`)
	bracketIP  = regexp.MustCompile(`\[([0-9a-fA-F:.]+)\]`)
)

// relayInfo extracts what the relaying MX (Cloudflare) recorded about the
// original sender: the topmost Received-SPF and Received headers are the
// relay's own, since each hop prepends.
func relayInfo(data []byte) (ip net.IP, helo string, spf mailauth.SPFResult) {
	hdr, _ := message.SplitHeader(data)
	var gotSPF, gotRecv bool
	for _, f := range message.ParseHeader(hdr) {
		switch {
		case !gotSPF && strings.EqualFold(f.Name, "Received-SPF"):
			gotSPF = true
			word, _, _ := strings.Cut(strings.TrimSpace(f.Value), " ")
			switch r := mailauth.SPFResult(strings.ToLower(word)); r {
			case mailauth.SPFPass, mailauth.SPFFail, mailauth.SPFSoftFail, mailauth.SPFNeutral,
				mailauth.SPFNone, mailauth.SPFTempError, mailauth.SPFPermError:
				spf = r
			}
			if m := clientIPRe.FindStringSubmatch(f.Value); m != nil {
				ip = net.ParseIP(m[1])
			}
			if m := heloRe.FindStringSubmatch(f.Value); m != nil {
				helo = m[1]
			}
		case !gotRecv && strings.EqualFold(f.Name, "Received"):
			gotRecv = true
			if ip == nil {
				if m := bracketIP.FindStringSubmatch(f.Value); m != nil {
					ip = net.ParseIP(m[1])
				}
			}
			if helo == "" {
				if rest, ok := strings.CutPrefix(f.Value, "from "); ok {
					helo, _, _ = strings.Cut(rest, " ")
				}
			}
		}
	}
	return ip, helo, spf
}

func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x21 || r > 0x7e || r == '(' || r == ')' {
			return -1
		}
		return r
	}, s)
}

// stripOwnAuthResults removes Authentication-Results headers that claim to
// come from us, which a sender could forge to make mail look verified.
func stripOwnAuthResults(data []byte, hostname string) []byte {
	hdr, body := message.SplitHeader(data)
	fields := message.ParseHeader(hdr)
	var out bytes.Buffer
	for _, f := range fields {
		if strings.EqualFold(f.Name, "Authentication-Results") {
			id, _, _ := strings.Cut(f.Value, ";")
			if strings.EqualFold(strings.TrimSpace(id), hostname) {
				continue
			}
		}
		if strings.EqualFold(f.Name, "X-Mailer-Junk") {
			continue
		}
		out.Write(f.Raw)
	}
	if len(body) > 0 || bytes.HasSuffix(hdr, []byte("\r\n\r\n")) || bytes.Equal(hdr, []byte("\r\n")) {
		out.WriteString("\r\n")
	}
	out.Write(body)
	return out.Bytes()
}

// ---- submission ----

type Submission struct{ *MTA }

func (b Submission) CheckSender(env *smtp.Envelope) error {
	if b.Users.Locks(env.AuthUser).Send {
		return &smtp.Error{Code: 550, Enhanced: "5.7.1", Msg: "sending is locked for this account"}
	}
	if env.From == "" || !b.mayUse(env.AuthUser, env.From) {
		return &smtp.Error{Code: 553, Enhanced: "5.7.1", Msg: "sender address not owned by " + env.AuthUser}
	}
	return nil
}

func (b Submission) CheckRecipient(env *smtp.Envelope, rcpt string) error {
	cfg := b.Cfg.Get()
	_, domain := splitAddr(rcpt)
	if domain == "" {
		return &smtp.Error{Code: 501, Enhanced: "5.1.3", Msg: "invalid recipient"}
	}
	if cfg.IsLocalDomain(domain) {
		user, ok := b.Resolve(rcpt)
		if !ok {
			return &smtp.Error{Code: 550, Enhanced: "5.1.1", Msg: "no such user"}
		}
		if b.Users.Locks(user).Receive {
			return &smtp.Error{Code: 550, Enhanced: "5.2.1", Msg: "mailbox is not accepting mail"}
		}
	}
	return nil
}

func (b Submission) Deliver(env *smtp.Envelope) error {
	cfg := b.Cfg.Get()
	hdrBytes, _ := message.SplitHeader(env.Data)
	header := message.ParseHeader(hdrBytes)

	froms, err := mail.ParseAddressList(header.Get("From"))
	if err != nil || len(froms) == 0 {
		return &smtp.Error{Code: 550, Enhanced: "5.6.0", Msg: "missing or invalid From header"}
	}
	for _, f := range froms {
		if !b.mayUse(env.AuthUser, f.Address) {
			return &smtp.Error{Code: 550, Enhanced: "5.7.1", Msg: "From " + f.Address + " not owned by " + env.AuthUser}
		}
	}
	_, fromDomain := splitAddr(froms[0].Address)

	var extra bytes.Buffer
	fmt.Fprintf(&extra, "Received: by %s with ESMTPSA;\r\n\t%s\r\n", cfg.Hostname, time.Now().Format(time.RFC1123Z))
	if !header.Has("Date") {
		fmt.Fprintf(&extra, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	}
	if !header.Has("Message-Id") {
		id := make([]byte, 12)
		rand.Read(id)
		fmt.Fprintf(&extra, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(id), cfg.Hostname)
	}
	data := append(extra.Bytes(), env.Data...)

	if s := b.DKIM(fromDomain); s != nil {
		sig, err := s.Sign(data)
		if err != nil {
			b.Log.Printf("dkim: signing failed: %v", err)
			return tempErr("signing failed")
		}
		data = append(sig, data...)
	}

	var remote []string
	delivered := map[string]bool{}
	for _, rcpt := range env.To {
		if user, ok := b.Resolve(rcpt); ok {
			if !delivered[user] {
				if err := b.deliverLocal(user, false, data); err != nil {
					return tempErr("local delivery failed")
				}
				delivered[user] = true
				b.Mail.Add(maillog.Entry{Dir: maillog.Out, From: env.From, To: rcpt, Size: len(data),
					Status: maillog.Delivered, Detail: "to " + user + " on this server"})
			}
			continue
		}
		remote = append(remote, rcpt)
	}
	if len(remote) > 0 {
		if err := b.Queue.Enqueue(env.From, remote, data); err != nil {
			b.Log.Printf("queue: enqueue failed: %v", err)
			return tempErr("could not queue message")
		}
	}
	if cfg.SaveSentCopy {
		if acct, err := b.Store.Account(env.AuthUser); err == nil {
			if sent := acct.FindSpecialUse(`\Sent`); sent != "" {
				acct.Append(sent, data, []string{store.FlagSeen}, time.Now())
			}
		}
	}
	b.Log.Printf("submission: user=%s from=%s local=%d remote=%d", env.AuthUser, env.From, len(delivered), len(remote))
	return nil
}

// ---- bounces ----

// Bounce delivers a delivery status notification to the local sender.
func (m *MTA) Bounce(it *queue.Item, failed []*queue.Rcpt, original []byte) {
	cfg := m.Cfg.Get()
	user, ok := m.Resolve(it.From)
	if !ok {
		return
	}
	boundary := make([]byte, 12)
	rand.Read(boundary)
	bnd := hex.EncodeToString(boundary)
	hdr, _ := message.SplitHeader(original)

	var b bytes.Buffer
	fmt.Fprintf(&b, "From: Mail Delivery System <MAILER-DAEMON@%s>\r\n", cfg.Hostname)
	fmt.Fprintf(&b, "To: <%s>\r\n", it.From)
	b.WriteString("Subject: Undelivered Mail Returned to Sender\r\n")
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s.bounce@%s>\r\n", bnd, cfg.Hostname)
	b.WriteString("Auto-Submitted: auto-replied\r\nMIME-Version: 1.0\r\n")
	fmt.Fprintf(&b, "Content-Type: multipart/report; report-type=delivery-status; boundary=\"%s\"\r\n\r\n", bnd)

	fmt.Fprintf(&b, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n", bnd)
	b.WriteString("Your message could not be delivered to the following recipients:\r\n\r\n")
	for _, r := range failed {
		fmt.Fprintf(&b, "  %s\r\n    %s\r\n\r\n", r.Addr, r.LastError)
	}

	fmt.Fprintf(&b, "--%s\r\nContent-Type: message/delivery-status\r\n\r\n", bnd)
	fmt.Fprintf(&b, "Reporting-MTA: dns; %s\r\nArrival-Date: %s\r\n", cfg.Hostname, it.Created.Format(time.RFC1123Z))
	for _, r := range failed {
		fmt.Fprintf(&b, "\r\nFinal-Recipient: rfc822; %s\r\nAction: failed\r\nStatus: 5.0.0\r\nDiagnostic-Code: smtp; %s\r\n",
			r.Addr, strings.ReplaceAll(r.LastError, "\n", " "))
	}
	fmt.Fprintf(&b, "\r\n--%s\r\nContent-Type: text/rfc822-headers\r\n\r\n", bnd)
	b.Write(hdr)
	fmt.Fprintf(&b, "--%s--\r\n", bnd)

	if err := m.deliverLocal(user, false, b.Bytes()); err != nil {
		m.Log.Printf("bounce: delivering to %s failed: %v", user, err)
	}
}
