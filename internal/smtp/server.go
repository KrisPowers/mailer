// Package smtp implements an ESMTP server (RFC 5321) for both inbound
// delivery (MX, port 25) and authenticated client submission (RFC 6409,
// ports 587/465).
package smtp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	"mailer/internal/audit"
	"mailer/internal/proxyproto"
	"mailer/internal/users"
)

type Mode int

const (
	ModeMX Mode = iota
	ModeSubmission
)

// Error is an SMTP reply returned by a Backend.
type Error struct {
	Code     int
	Enhanced string
	Msg      string
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s %s", e.Code, e.Enhanced, e.Msg) }

type Envelope struct {
	RemoteIP net.IP
	Helo     string
	TLS      bool
	AuthUser string // set for authenticated submission
	From     string // reverse-path; empty for bounces
	To       []string
	Data     []byte
}

type Backend interface {
	CheckSender(env *Envelope) error
	CheckRecipient(env *Envelope, rcpt string) error
	Deliver(env *Envelope) error
}

type Server struct {
	Hostname     func() string
	Mode         Mode
	TLSConfig    *tls.Config
	Users        *users.DB
	Limiter      *users.Limiter
	InsecureAuth bool
	MaxSize      int64
	Backend      Backend
	Log          *log.Logger
	Audit        *audit.Log // sign-ins on the submission port
	// Trace logs every command a client sends and every reply, with
	// sign-in secrets hidden.
	Trace bool
}

const (
	maxRecipients = 100
	maxErrors     = 10
	cmdTimeout    = 5 * time.Minute
	dataTimeout   = 10 * time.Minute
	maxCmdLine    = 4096
)

// Serve accepts connections until l is closed. If terminated is true, TLS
// was already terminated by a trusted local proxy (Tailscale Funnel) that
// may send a PROXY protocol header carrying the real client address.
func (srv *Server) Serve(l net.Listener, terminated bool) error {
	for {
		c, err := l.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}
		go srv.handle(c, terminated)
	}
}

type session struct {
	srv  *Server
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
	ip   net.IP
	errs int

	helo     string
	tls      bool
	authUser string
	env      *Envelope

	// For the sign-in audit: the commands sent before signing in, and
	// whether an attempt was recorded.
	cmds    []string
	audited bool
}

func (srv *Server) handle(c net.Conn, terminated bool) {
	s := &session{srv: srv, conn: c}
	if ta, ok := c.RemoteAddr().(*net.TCPAddr); ok {
		s.ip = ta.IP
	}
	defer func() {
		s.conn.Close()
		if !s.audited && len(s.cmds) > 0 {
			detail := "disconnected without signing in"
			if slices.Contains(s.cmds, "MAIL") {
				detail = "tried to send without signing in; turn on the app's \"outgoing server requires authentication\""
			}
			s.audit("", audit.NoSignIn, detail)
		}
	}()
	if terminated {
		pc, ip, err := proxyproto.Read(c, 2*time.Second)
		if err != nil {
			return
		}
		s.conn, s.ip, s.tls = pc, ip, true
	}
	s.r = bufio.NewReader(s.conn)
	s.w = bufio.NewWriter(s.conn)
	s.reply(220, "", srv.Hostname()+" ESMTP ready")

	for {
		s.conn.SetReadDeadline(time.Now().Add(cmdTimeout))
		line, err := s.readLine(maxCmdLine)
		if err != nil {
			if s.srv.Trace {
				s.srv.Log.Printf("smtp trace %s: read ended: %v", s.ip, err)
			}
			if errors.Is(err, errLineTooLong) {
				s.reply(500, "5.5.2", "line too long")
			}
			return
		}
		verb, arg, _ := strings.Cut(string(line), " ")
		if s.srv.Trace {
			shown := string(line)
			if strings.EqualFold(verb, "AUTH") {
				shown = verb + " " + strings.Fields(arg + " x")[0] + " [hidden]"
			}
			s.srv.Log.Printf("smtp trace %s < %s", s.ip, printable(shown, 300))
		}
		if quit := s.command(strings.ToUpper(verb), strings.TrimSpace(arg)); quit {
			return
		}
		if s.errs >= maxErrors {
			s.reply(421, "4.7.0", "too many errors")
			return
		}
	}
}

var errLineTooLong = errors.New("line too long")

func (s *session) readLine(max int) ([]byte, error) {
	var line []byte
	for {
		chunk, err := s.r.ReadSlice('\n')
		line = append(line, chunk...)
		if max > 0 && len(line) > max {
			return nil, errLineTooLong
		}
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return nil, err
		}
		break
	}
	line = bytes.TrimSuffix(line, []byte("\n"))
	return bytes.TrimSuffix(line, []byte("\r")), nil
}

func (s *session) reply(code int, enhanced, msg string) {
	if s.srv.Trace {
		s.srv.Log.Printf("smtp trace %s > %d %s %s", s.ip, code, enhanced, strings.ReplaceAll(msg, "\n", " | "))
	}
	lines := strings.Split(msg, "\n")
	for i, l := range lines {
		sep := "-"
		if i == len(lines)-1 {
			sep = " "
		}
		if enhanced != "" {
			fmt.Fprintf(s.w, "%d%s%s %s\r\n", code, sep, enhanced, l)
		} else {
			fmt.Fprintf(s.w, "%d%s%s\r\n", code, sep, l)
		}
	}
	s.w.Flush()
	if code >= 500 {
		s.errs++
	}
}

func (s *session) replyErr(err error) {
	var se *Error
	if errors.As(err, &se) {
		s.reply(se.Code, se.Enhanced, se.Msg)
		return
	}
	s.srv.Log.Printf("smtp: internal error: %v", err)
	s.reply(451, "4.3.0", "temporary server error")
}

func (s *session) canAuth() bool {
	return s.srv.Mode == ModeSubmission && (s.tls || s.srv.InsecureAuth)
}

// audit records a sign-in attempt on the submission port, or a connection
// that left without one.
func (s *session) audit(user, res, detail string) {
	s.audited = true
	if s.srv.Mode != ModeSubmission {
		return
	}
	s.srv.Audit.Add(audit.Event{Proto: "smtp", IP: s.ip.String(), Client: printable(s.helo, 100),
		User: printable(user, 200), Result: res, Detail: detail, Commands: s.cmds})
}

// printable keeps a client-supplied string printable and short.
func printable(v string, n int) string {
	v = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, v)
	if len(v) > n {
		v = v[:n]
	}
	return v
}

func (s *session) command(verb, arg string) (quit bool) {
	if s.authUser == "" && len(s.cmds) < 20 {
		c := printable(verb, 20)
		if verb == "AUTH" {
			mech, _, _ := strings.Cut(arg, " ")
			c += " " + printable(strings.ToUpper(mech), 40)
		}
		s.cmds = append(s.cmds, c)
	}
	switch verb {
	case "EHLO", "HELO":
		if arg == "" {
			s.reply(501, "5.5.4", "domain required")
			return
		}
		s.helo, s.env = arg, nil
		if verb == "HELO" {
			s.reply(250, "", s.srv.Hostname())
			return
		}
		ext := []string{s.srv.Hostname(), "PIPELINING", "8BITMIME", "ENHANCEDSTATUSCODES", "SMTPUTF8",
			fmt.Sprintf("SIZE %d", s.srv.MaxSize)}
		if !s.tls && s.srv.TLSConfig != nil {
			ext = append(ext, "STARTTLS")
		}
		if s.canAuth() {
			ext = append(ext, "AUTH PLAIN LOGIN")
		}
		s.reply(250, "", strings.Join(ext, "\n"))

	case "STARTTLS":
		if s.tls || s.srv.TLSConfig == nil {
			s.reply(502, "5.5.1", "TLS not available")
			return
		}
		if s.r.Buffered() > 0 {
			s.reply(501, "5.5.4", "pipelining after STARTTLS not allowed")
			return true
		}
		s.reply(220, "2.0.0", "ready to start TLS")
		tc := tls.Server(s.conn, s.srv.TLSConfig)
		tc.SetDeadline(time.Now().Add(30 * time.Second))
		if err := tc.Handshake(); err != nil {
			return true
		}
		tc.SetDeadline(time.Time{})
		s.conn, s.tls = tc, true
		s.r, s.w = bufio.NewReader(tc), bufio.NewWriter(tc)
		s.helo, s.env, s.authUser = "", nil, ""

	case "AUTH":
		s.auth(arg)

	case "MAIL":
		s.mail(arg)

	case "RCPT":
		s.rcpt(arg)

	case "DATA":
		s.data()

	case "RSET":
		s.env = nil
		s.reply(250, "2.0.0", "reset")
	case "NOOP":
		s.reply(250, "2.0.0", "ok")
	case "VRFY":
		s.reply(252, "2.5.0", "cannot verify, but will attempt delivery")
	case "HELP":
		s.reply(214, "2.0.0", "see RFC 5321")
	case "QUIT":
		s.reply(221, "2.0.0", "bye")
		return true
	default:
		s.reply(500, "5.5.1", "command not recognized")
	}
	return false
}

func (s *session) auth(arg string) {
	if !s.canAuth() {
		if s.srv.Mode == ModeSubmission {
			s.audit("", audit.NeedsTLS, "tried to sign in without TLS")
		}
		s.reply(503, "5.5.1", "AUTH not available")
		return
	}
	if s.authUser != "" {
		s.reply(503, "5.5.1", "already authenticated")
		return
	}
	if s.env != nil {
		s.reply(503, "5.5.1", "AUTH not permitted during a transaction")
		return
	}
	ipStr := s.ip.String()
	if s.srv.Limiter.Blocked(ipStr) {
		s.audit("", audit.Blocked, "too many failed sign-ins from this address in the last 15 minutes")
		s.reply(454, "4.7.0", "too many failed attempts; try later")
		return
	}
	mech, initial, _ := strings.Cut(arg, " ")
	readResp := func(prompt string) (string, bool) {
		fmt.Fprintf(s.w, "334 %s\r\n", prompt)
		s.w.Flush()
		line, err := s.readLine(maxCmdLine)
		if err != nil || string(line) == "*" {
			return "", false
		}
		return string(line), true
	}
	decode := func(v string) (string, bool) {
		if v == "=" {
			return "", true
		}
		b, err := base64.StdEncoding.DecodeString(v)
		return string(b), err == nil
	}

	var user, pass string
	switch strings.ToUpper(mech) {
	case "PLAIN":
		resp := initial
		if resp == "" {
			var ok bool
			if resp, ok = readResp(""); !ok {
				s.audit("", audit.Malformed, "the app cancelled AUTH PLAIN")
				s.reply(501, "5.0.0", "authentication cancelled")
				return
			}
		}
		dec, ok := decode(resp)
		parts := strings.Split(dec, "\x00")
		if !ok || len(parts) != 3 || (parts[0] != "" && !strings.EqualFold(parts[0], parts[1])) {
			s.audit("", audit.Malformed, "AUTH PLAIN credentials aren't in the expected form")
			s.reply(501, "5.5.2", "malformed credentials")
			return
		}
		user, pass = parts[1], parts[2]
	case "LOGIN":
		u := initial
		var ok bool
		if u == "" {
			if u, ok = readResp("VXNlcm5hbWU6"); !ok { // "Username:"
				s.audit("", audit.Malformed, "the app cancelled AUTH LOGIN")
				s.reply(501, "5.0.0", "authentication cancelled")
				return
			}
		}
		p, ok := readResp("UGFzc3dvcmQ6") // "Password:"
		if !ok {
			s.audit("", audit.Malformed, "the app cancelled AUTH LOGIN")
			s.reply(501, "5.0.0", "authentication cancelled")
			return
		}
		var ok1, ok2 bool
		user, ok1 = decode(u)
		pass, ok2 = decode(p)
		if !ok1 || !ok2 {
			s.audit("", audit.Malformed, "AUTH LOGIN credentials aren't base64")
			s.reply(501, "5.5.2", "malformed credentials")
			return
		}
	default:
		s.audit("", audit.Unsupported, "asked for AUTH "+printable(mech, 40)+"; only PLAIN and LOGIN are offered")
		s.reply(504, "5.5.4", "unsupported mechanism")
		return
	}
	email, ok := s.srv.Users.Authenticate(user, pass)
	if !ok {
		res, detail := audit.BadPassword, "wrong password"
		if !s.srv.Users.Exists(user) {
			res, detail = audit.UnknownUser, "no account with this username; use the full email address"
		}
		if s.srv.Limiter.Fail(ipStr) {
			detail += "; this address is now blocked for 15 minutes"
		}
		s.audit(user, res, detail)
		s.srv.Log.Printf("smtp: failed auth for %q from %s (%s)", user, ipStr, detail)
		s.reply(535, "5.7.8", "authentication failed")
		return
	}
	s.authUser = email
	s.audit(email, audit.OK, "")
	s.reply(235, "2.7.0", "authenticated")
}

// parsePath parses "FROM:<addr> PARAMS" style arguments.
func parsePath(arg, prefix string) (addr string, params map[string]string, err error) {
	if len(arg) < len(prefix) || !strings.EqualFold(arg[:len(prefix)], prefix) {
		return "", nil, errors.New("syntax")
	}
	rest := strings.TrimSpace(arg[len(prefix):])
	if !strings.HasPrefix(rest, "<") {
		return "", nil, errors.New("syntax")
	}
	end := strings.IndexByte(rest, '>')
	if end < 0 {
		return "", nil, errors.New("syntax")
	}
	addr = rest[1:end]
	if i := strings.IndexByte(addr, ':'); i >= 0 && strings.HasPrefix(addr, "@") {
		addr = addr[i+1:] // strip source route
	}
	params = map[string]string{}
	for _, p := range strings.Fields(rest[end+1:]) {
		k, v, _ := strings.Cut(p, "=")
		params[strings.ToUpper(k)] = v
	}
	if addr != "" && (strings.Count(addr, "@") < 1 || strings.ContainsAny(addr, " \t<>\r\n")) {
		return "", nil, errors.New("bad address")
	}
	return addr, params, nil
}

func (s *session) mail(arg string) {
	if s.helo == "" {
		s.reply(503, "5.5.1", "send EHLO first")
		return
	}
	if s.env != nil {
		s.reply(503, "5.5.1", "nested MAIL command")
		return
	}
	if s.srv.Mode == ModeSubmission && s.authUser == "" {
		s.reply(530, "5.7.0", "authentication required")
		return
	}
	addr, params, err := parsePath(arg, "FROM:")
	if err != nil {
		s.reply(501, "5.1.7", "invalid sender address")
		return
	}
	if sz, ok := params["SIZE"]; ok {
		n, err := strconv.ParseInt(sz, 10, 64)
		if err == nil && n > s.srv.MaxSize {
			s.reply(552, "5.3.4", "message too large")
			return
		}
	}
	env := &Envelope{RemoteIP: s.ip, Helo: s.helo, TLS: s.tls, AuthUser: s.authUser, From: addr}
	if err := s.srv.Backend.CheckSender(env); err != nil {
		s.replyErr(err)
		return
	}
	s.env = env
	s.reply(250, "2.1.0", "sender ok")
}

func (s *session) rcpt(arg string) {
	if s.env == nil {
		s.reply(503, "5.5.1", "send MAIL first")
		return
	}
	if len(s.env.To) >= maxRecipients {
		s.reply(452, "4.5.3", "too many recipients")
		return
	}
	addr, _, err := parsePath(arg, "TO:")
	if err != nil || addr == "" {
		s.reply(501, "5.1.3", "invalid recipient address")
		return
	}
	if err := s.srv.Backend.CheckRecipient(s.env, addr); err != nil {
		s.replyErr(err)
		return
	}
	s.env.To = append(s.env.To, addr)
	s.reply(250, "2.1.5", "recipient ok")
}

func (s *session) data() {
	if s.env == nil || len(s.env.To) == 0 {
		s.reply(503, "5.5.1", "need RCPT first")
		return
	}
	s.reply(354, "", "end data with <CR><LF>.<CR><LF>")
	s.conn.SetReadDeadline(time.Now().Add(dataTimeout))
	var buf bytes.Buffer
	tooBig := false
	for {
		line, err := s.readLine(0)
		if err != nil {
			if err != io.EOF {
				s.srv.Log.Printf("smtp: data read error from %s: %v", s.ip, err)
			}
			s.env = nil
			return
		}
		if len(line) == 1 && line[0] == '.' {
			break
		}
		if len(line) > 0 && line[0] == '.' {
			line = line[1:]
		}
		if int64(buf.Len()+len(line)+2) > s.srv.MaxSize {
			tooBig = true
			continue
		}
		buf.Write(line)
		buf.WriteString("\r\n")
	}
	env := s.env
	s.env = nil
	if tooBig {
		s.reply(552, "5.3.4", "message too large")
		return
	}
	env.Data = buf.Bytes()
	if err := s.srv.Backend.Deliver(env); err != nil {
		s.replyErr(err)
		return
	}
	s.reply(250, "2.0.0", "message accepted")
}
