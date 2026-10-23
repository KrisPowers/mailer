// Package imap implements an IMAP4rev1 server (RFC 3501) with the extensions
// Outlook and the Gmail app rely on: IDLE, UIDPLUS, MOVE, SPECIAL-USE,
// LITERAL+, SASL-IR, NAMESPACE, UNSELECT, ID, ENABLE.
package imap

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
	"strings"
	"time"

	"mailer/internal/message"
	"mailer/internal/proxyproto"
	"mailer/internal/store"
	"mailer/internal/users"
)

type Server struct {
	Hostname     func() string
	TLSConfig    *tls.Config
	Users        *users.DB
	Store        *store.Store
	Limiter      *users.Limiter
	InsecureAuth bool
	MaxMessage   int64
	Log          *log.Logger
}

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

const (
	maxLine           = 64 << 10
	preAuthMaxLiteral = 8 << 10
	idleTimeout       = 30 * time.Minute
	dateTimeLayout    = "_2-Jan-2006 15:04:05 -0700"
)

var (
	errTooBig = errors.New("literal too large")
	errLine   = errors.New("line too long")
)

type session struct {
	srv  *Server
	conn net.Conn
	r    *bufio.Reader
	w    *bufio.Writer
	ip   string
	tls  bool

	user    string
	acct    *store.Account
	watch   <-chan struct{}
	unwatch func()

	// Selected mailbox state. uids[i] is the UID of sequence number i+1 as
	// this session currently knows it; flags caches the last flags we sent.
	mailbox  string
	readOnly bool
	uids     []uint32
	flags    map[uint32]string
}

type result struct{ status, text string }

func ok(text string) result  { return result{"OK", text} }
func no(text string) result  { return result{"NO", text} }
func bad(text string) result { return result{"BAD", text} }

func (srv *Server) handle(c net.Conn, terminated bool) {
	s := &session{srv: srv, conn: c}
	s.ip, _, _ = net.SplitHostPort(c.RemoteAddr().String())
	defer func() {
		if s.unwatch != nil {
			s.unwatch()
		}
		s.conn.Close()
	}()
	if terminated {
		pc, ip, err := proxyproto.Read(c, 2*time.Second)
		if err != nil {
			return
		}
		s.conn, s.ip, s.tls = pc, ip.String(), true
	}
	s.r = bufio.NewReader(s.conn)
	s.w = bufio.NewWriter(s.conn)
	s.writef("* OK [CAPABILITY %s] %s IMAP4rev1 ready\r\n", s.caps(), srv.Hostname())
	s.w.Flush()

	for {
		s.conn.SetReadDeadline(time.Now().Add(idleTimeout))
		cmdBytes, err := s.readCommand()
		if err != nil {
			if errors.Is(err, errTooBig) || errors.Is(err, errLine) {
				s.writef("* BYE %s\r\n", err)
			} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
				s.writef("* BYE autologout\r\n")
			}
			s.w.Flush()
			return
		}
		tag, rest, _ := bytes.Cut(cmdBytes, []byte(" "))
		if len(tag) == 0 || bytes.ContainsAny(tag, "(){%*\"\\]+") {
			s.writef("* BAD invalid tag\r\n")
			s.w.Flush()
			continue
		}
		name, argBytes, _ := bytes.Cut(rest, []byte(" "))
		done := s.dispatch(string(tag), strings.ToUpper(string(name)), argBytes)
		if err := s.w.Flush(); err != nil || done {
			return
		}
	}
}

func (s *session) writef(format string, a ...any) {
	fmt.Fprintf(s.w, format, a...)
}

func (s *session) caps() string {
	c := "IMAP4rev1 LITERAL+ SASL-IR ID ENABLE IDLE NAMESPACE UIDPLUS MOVE UNSELECT SPECIAL-USE CHILDREN"
	if !s.tls && s.srv.TLSConfig != nil {
		c += " STARTTLS"
	}
	if s.tls || s.srv.InsecureAuth {
		c += " AUTH=PLAIN"
	} else {
		c += " LOGINDISABLED"
	}
	return c
}

func (s *session) readLine() ([]byte, error) {
	var line []byte
	for {
		chunk, err := s.r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxLine {
			return nil, errLine
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

// readCommand reads one complete command including any literals.
func (s *session) readCommand() ([]byte, error) {
	limit := int64(preAuthMaxLiteral)
	if s.user != "" {
		limit = s.srv.MaxMessage + 64<<10
	}
	var buf []byte
	var total int64
	for {
		line, err := s.readLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, line...)
		n, nonSync, isLit := literalSuffix(line)
		if !isLit {
			return buf, nil
		}
		total += n
		if total > limit {
			return nil, errTooBig
		}
		if !nonSync {
			s.writef("+ Ready for literal data\r\n")
			s.w.Flush()
		}
		buf = append(buf, '\r', '\n')
		data := make([]byte, n)
		if _, err := io.ReadFull(s.r, data); err != nil {
			return nil, err
		}
		buf = append(buf, data...)
	}
}

func (s *session) dispatch(tag, cmd string, argBytes []byte) (done bool) {
	// IDLE and AUTHENTICATE read further lines themselves.
	switch cmd {
	case "IDLE":
		if s.user == "" {
			s.writef("%s BAD not authenticated\r\n", tag)
			return false
		}
		return s.idle(tag)
	}

	args, err := parseArgs(argBytes)
	if err != nil {
		s.writef("%s BAD %s\r\n", tag, err)
		return false
	}

	uid := false
	if cmd == "UID" {
		if len(args) == 0 || args[0].kind != tAtom {
			s.writef("%s BAD missing UID command\r\n", tag)
			return false
		}
		uid = true
		cmd = strings.ToUpper(args[0].s)
		args = args[1:]
		switch cmd {
		case "FETCH", "STORE", "COPY", "MOVE", "SEARCH", "EXPUNGE":
		default:
			s.writef("%s BAD unknown UID command\r\n", tag)
			return false
		}
	}

	var res result
	switch cmd {
	case "CAPABILITY":
		s.writef("* CAPABILITY %s\r\n", s.caps())
		res = ok("CAPABILITY completed")
	case "NOOP", "CHECK":
		res = ok(cmd + " completed")
	case "LOGOUT":
		s.writef("* BYE logging out\r\n%s OK LOGOUT completed\r\n", tag)
		return true
	case "ID":
		s.writef("* ID (\"name\" \"mailer\")\r\n")
		res = ok("ID completed")
	case "STARTTLS":
		return s.startTLS(tag)
	case "LOGIN":
		res = s.login(args)
	case "AUTHENTICATE":
		res = s.authenticate(args)
	default:
		if s.user == "" {
			res = bad("not authenticated")
			break
		}
		res = s.authenticated(cmd, args, uid)
	}

	// Report changes made by other sessions (and our own expunges). EXPUNGE
	// responses are forbidden during non-UID FETCH, STORE and SEARCH because
	// they would shift the sequence numbers the client is using.
	allowExpunge := uid || (cmd != "FETCH" && cmd != "STORE" && cmd != "SEARCH")
	s.sync(allowExpunge)
	s.writef("%s %s %s\r\n", tag, res.status, res.text)
	return false
}

func (s *session) authenticated(cmd string, args []token, uid bool) result {
	switch cmd {
	case "ENABLE":
		s.writef("* ENABLED\r\n")
		return ok("ENABLE completed")
	case "NAMESPACE":
		s.writef("* NAMESPACE ((\"\" \"/\")) NIL NIL\r\n")
		return ok("NAMESPACE completed")
	case "SELECT", "EXAMINE":
		if len(args) < 1 {
			return bad("missing mailbox")
		}
		return s.selectMailbox(args[0].s, cmd == "EXAMINE")
	case "CREATE":
		if len(args) < 1 {
			return bad("missing mailbox")
		}
		if err := s.acct.Create(strings.TrimSuffix(args[0].s, "/")); err != nil {
			return no(err.Error())
		}
		return ok("CREATE completed")
	case "DELETE":
		if len(args) < 1 {
			return bad("missing mailbox")
		}
		if strings.EqualFold(args[0].s, s.mailbox) || args[0].s == s.mailbox {
			s.deselect()
		}
		if err := s.acct.Delete(args[0].s); err != nil {
			return no(err.Error())
		}
		return ok("DELETE completed")
	case "RENAME":
		if len(args) < 2 {
			return bad("missing arguments")
		}
		if err := s.acct.Rename(args[0].s, args[1].s); err != nil {
			return no(err.Error())
		}
		return ok("RENAME completed")
	case "SUBSCRIBE", "UNSUBSCRIBE":
		if len(args) < 1 {
			return bad("missing mailbox")
		}
		if err := s.acct.SetSubscribed(args[0].s, cmd == "SUBSCRIBE"); err != nil {
			return no(err.Error())
		}
		return ok(cmd + " completed")
	case "LIST", "LSUB":
		return s.list(args, cmd == "LSUB")
	case "STATUS":
		return s.status(args)
	case "APPEND":
		return s.appendMsg(args)
	}

	if s.mailbox == "" {
		switch cmd {
		case "CLOSE", "UNSELECT", "EXPUNGE", "SEARCH", "FETCH", "STORE", "COPY", "MOVE":
			return bad("no mailbox selected")
		}
		return bad("unknown command")
	}
	switch cmd {
	case "CLOSE":
		if !s.readOnly {
			s.acct.Expunge(s.mailbox, nil)
		}
		s.deselect()
		return ok("CLOSE completed")
	case "UNSELECT":
		s.deselect()
		return ok("UNSELECT completed")
	case "EXPUNGE":
		return s.expunge(args, uid)
	case "SEARCH":
		return s.search(args, uid)
	case "FETCH":
		return s.fetch(args, uid)
	case "STORE":
		return s.store(args, uid)
	case "COPY", "MOVE":
		return s.copyMove(args, uid, cmd == "MOVE")
	}
	return bad("unknown command")
}

// ---- authentication ----

func (s *session) authAllowed() result {
	if !s.tls && !s.srv.InsecureAuth {
		return no("[PRIVACYREQUIRED] use STARTTLS or port 993")
	}
	if s.user != "" {
		return bad("already authenticated")
	}
	if s.srv.Limiter.Blocked(s.ip) {
		return no("[UNAVAILABLE] too many failed logins; try later")
	}
	return result{}
}

func (s *session) login(args []token) result {
	if r := s.authAllowed(); r.status != "" {
		return r
	}
	if len(args) != 2 {
		return bad("LOGIN requires username and password")
	}
	return s.finishAuth(args[0].s, args[1].s)
}

func (s *session) authenticate(args []token) result {
	if r := s.authAllowed(); r.status != "" {
		return r
	}
	if len(args) < 1 || !strings.EqualFold(args[0].s, "PLAIN") {
		return no("[CANNOT] only PLAIN is supported")
	}
	var resp string
	if len(args) >= 2 {
		resp = args[1].s // SASL-IR
	} else {
		s.writef("+ \r\n")
		s.w.Flush()
		line, err := s.readLine()
		if err != nil {
			return bad("read error")
		}
		resp = string(line)
	}
	if resp == "*" {
		return bad("authentication cancelled")
	}
	if resp == "=" {
		resp = ""
	}
	dec, err := base64.StdEncoding.DecodeString(resp)
	if err != nil {
		return bad("invalid base64")
	}
	parts := strings.Split(string(dec), "\x00")
	if len(parts) != 3 || (parts[0] != "" && !strings.EqualFold(parts[0], parts[1])) {
		s.srv.Limiter.Fail(s.ip)
		return no("[AUTHENTICATIONFAILED] invalid credentials")
	}
	return s.finishAuth(parts[1], parts[2])
}

func (s *session) finishAuth(user, pass string) result {
	email, okAuth := s.srv.Users.Authenticate(user, pass)
	if !okAuth {
		s.srv.Limiter.Fail(s.ip)
		s.srv.Log.Printf("imap: failed login for %q from %s", user, s.ip)
		return no("[AUTHENTICATIONFAILED] invalid credentials")
	}
	acct, err := s.srv.Store.Account(email)
	if err != nil {
		return no("[SERVERBUG] " + err.Error())
	}
	s.user, s.acct = email, acct
	s.watch, s.unwatch = acct.Watch()
	return ok(fmt.Sprintf("[CAPABILITY %s] logged in", s.caps()))
}

func (s *session) startTLS(tag string) bool {
	if s.tls || s.srv.TLSConfig == nil {
		s.writef("%s BAD TLS not available\r\n", tag)
		return false
	}
	if s.r.Buffered() > 0 {
		// Plaintext pipelined after STARTTLS is a command injection attempt.
		s.writef("%s BAD unexpected data after STARTTLS\r\n", tag)
		return true
	}
	s.writef("%s OK begin TLS negotiation\r\n", tag)
	s.w.Flush()
	tc := tls.Server(s.conn, s.srv.TLSConfig)
	tc.SetDeadline(time.Now().Add(30 * time.Second))
	if err := tc.Handshake(); err != nil {
		return true
	}
	tc.SetDeadline(time.Time{})
	s.conn, s.tls = tc, true
	s.r = bufio.NewReader(tc)
	s.w = bufio.NewWriter(tc)
	return false
}

// ---- mailbox state ----

func flagString(flags []string) string { return strings.Join(flags, " ") }

func (s *session) deselect() {
	s.mailbox, s.readOnly, s.uids, s.flags = "", false, nil, nil
}

func (s *session) selectMailbox(name string, readOnly bool) result {
	s.deselect()
	snap, err := s.acct.Snapshot(name)
	if err != nil {
		return no(err.Error())
	}
	s.mailbox, s.readOnly = snap.Name, readOnly
	s.flags = map[uint32]string{}
	firstUnseen := 0
	for i, m := range snap.Messages {
		s.uids = append(s.uids, m.UID)
		s.flags[m.UID] = flagString(m.Flags)
		if firstUnseen == 0 && !m.HasFlag(store.FlagSeen) {
			firstUnseen = i + 1
		}
	}
	s.writef("* FLAGS (\\Answered \\Flagged \\Deleted \\Seen \\Draft)\r\n")
	s.writef("* OK [PERMANENTFLAGS (\\Answered \\Flagged \\Deleted \\Seen \\Draft \\*)] flags permitted\r\n")
	s.writef("* %d EXISTS\r\n* 0 RECENT\r\n", len(s.uids))
	if firstUnseen > 0 {
		s.writef("* OK [UNSEEN %d] first unseen\r\n", firstUnseen)
	}
	s.writef("* OK [UIDVALIDITY %d] UIDs valid\r\n", snap.UIDValidity)
	s.writef("* OK [UIDNEXT %d] predicted next UID\r\n", snap.UIDNext)
	if readOnly {
		return ok("[READ-ONLY] EXAMINE completed")
	}
	return ok("[READ-WRITE] SELECT completed")
}

// sync reports changes to the selected mailbox since this session last
// looked: expunges, new messages, and flag changes.
func (s *session) sync(allowExpunge bool) {
	if s.mailbox == "" {
		return
	}
	snap, err := s.acct.Snapshot(s.mailbox)
	if err != nil {
		return
	}
	current := make(map[uint32]*store.MessageInfo, len(snap.Messages))
	for _, m := range snap.Messages {
		current[m.UID] = m
	}
	if allowExpunge {
		for i := len(s.uids) - 1; i >= 0; i-- {
			if _, ok := current[s.uids[i]]; !ok {
				s.writef("* %d EXPUNGE\r\n", i+1)
				delete(s.flags, s.uids[i])
				s.uids = append(s.uids[:i], s.uids[i+1:]...)
			}
		}
	}
	var last uint32
	if len(s.uids) > 0 {
		last = s.uids[len(s.uids)-1]
	}
	added := false
	for _, m := range snap.Messages {
		if m.UID > last {
			s.uids = append(s.uids, m.UID)
			s.flags[m.UID] = flagString(m.Flags)
			added = true
		}
	}
	if added {
		s.writef("* %d EXISTS\r\n", len(s.uids))
	}
	for i, u := range s.uids {
		m, ok := current[u]
		if !ok {
			continue
		}
		if f := flagString(m.Flags); f != s.flags[u] {
			s.flags[u] = f
			s.writef("* %d FETCH (UID %d FLAGS (%s))\r\n", i+1, u, f)
		}
	}
}

// resolve returns the indexes into s.uids selected by set.
func (s *session) resolve(set seqSet, uid bool) []int {
	if len(s.uids) == 0 {
		return nil
	}
	var out []int
	maxUID := s.uids[len(s.uids)-1]
	for i, u := range s.uids {
		if (uid && set.contains(u, maxUID)) || (!uid && set.contains(uint32(i+1), uint32(len(s.uids)))) {
			out = append(out, i)
		}
	}
	return out
}

func (s *session) resolveUIDs(arg token, uid bool) ([]uint32, error) {
	set, err := parseSeqSet(arg.s)
	if err != nil {
		return nil, err
	}
	var out []uint32
	for _, i := range s.resolve(set, uid) {
		out = append(out, s.uids[i])
	}
	return out, nil
}

// ---- LIST / STATUS / APPEND ----

func (s *session) list(args []token, lsub bool) result {
	var selectSubscribed, selectSpecial, returnSubscribed bool
	if len(args) > 0 && args[0].kind == tList { // LIST-EXTENDED selection options
		for _, o := range args[0].list {
			switch strings.ToUpper(o.s) {
			case "SUBSCRIBED":
				selectSubscribed, returnSubscribed = true, true
			case "SPECIAL-USE":
				selectSpecial = true
			}
		}
		args = args[1:]
	}
	if len(args) < 2 {
		return bad("LIST requires reference and pattern")
	}
	ref := args[0].s
	var patterns []string
	if args[1].kind == tList {
		for _, p := range args[1].list {
			patterns = append(patterns, p.s)
		}
	} else {
		patterns = []string{args[1].s}
	}
	if len(args) >= 4 && strings.EqualFold(args[2].s, "RETURN") && args[3].kind == tList {
		for _, o := range args[3].list {
			if strings.EqualFold(o.s, "SUBSCRIBED") {
				returnSubscribed = true
			}
		}
	}
	if lsub {
		selectSubscribed = true
	}
	resp := "LIST"
	if lsub {
		resp = "LSUB"
	}

	if len(patterns) == 1 && patterns[0] == "" {
		s.writef("* %s (\\Noselect) \"/\" \"\"\r\n", resp)
		return ok(resp + " completed")
	}

	boxes := s.acct.List()
	for _, mb := range boxes {
		matched := false
		for _, p := range patterns {
			full := ref + p
			name := mb.Name
			if strings.HasPrefix(strings.ToUpper(full), "INBOX") && name == "INBOX" {
				full = "INBOX" + full[5:]
			}
			if matchPattern(full, name) {
				matched = true
			}
		}
		if !matched || (selectSubscribed && !mb.Subscribed) || (selectSpecial && mb.SpecialUse == "") {
			continue
		}
		var attrs []string
		hasChildren := false
		for _, other := range boxes {
			if strings.HasPrefix(other.Name, mb.Name+"/") {
				hasChildren = true
				break
			}
		}
		if hasChildren {
			attrs = append(attrs, `\HasChildren`)
		} else {
			attrs = append(attrs, `\HasNoChildren`)
		}
		if mb.SpecialUse != "" {
			attrs = append(attrs, mb.SpecialUse)
		}
		if returnSubscribed && mb.Subscribed {
			attrs = append(attrs, `\Subscribed`)
		}
		s.writef("* %s (%s) \"/\" %s\r\n", resp, strings.Join(attrs, " "), quote(mb.Name))
	}
	return ok(resp + " completed")
}

func (s *session) status(args []token) result {
	if len(args) < 2 || args[1].kind != tList {
		return bad("STATUS requires mailbox and item list")
	}
	st, err := s.acct.Status(args[0].s)
	if err != nil {
		return no(err.Error())
	}
	var items []string
	for _, it := range args[1].list {
		switch strings.ToUpper(it.s) {
		case "MESSAGES":
			items = append(items, fmt.Sprintf("MESSAGES %d", st.Messages))
		case "RECENT":
			items = append(items, "RECENT 0")
		case "UIDNEXT":
			items = append(items, fmt.Sprintf("UIDNEXT %d", st.UIDNext))
		case "UIDVALIDITY":
			items = append(items, fmt.Sprintf("UIDVALIDITY %d", st.UIDValidity))
		case "UNSEEN":
			items = append(items, fmt.Sprintf("UNSEEN %d", st.Unseen))
		case "SIZE":
			items = append(items, fmt.Sprintf("SIZE %d", st.Size))
		default:
			return bad("unknown status item " + it.s)
		}
	}
	s.writef("* STATUS %s (%s)\r\n", quote(args[0].s), strings.Join(items, " "))
	return ok("STATUS completed")
}

func (s *session) appendMsg(args []token) result {
	if len(args) < 2 {
		return bad("APPEND requires mailbox and message")
	}
	name := args[0].s
	msg := args[len(args)-1]
	if msg.kind != tString {
		return bad("message must be a literal")
	}
	var flags []string
	var date time.Time
	for _, a := range args[1 : len(args)-1] {
		switch a.kind {
		case tList:
			for _, f := range a.list {
				flags = append(flags, f.s)
			}
		case tString:
			d, err := time.Parse(dateTimeLayout, a.s)
			if err != nil {
				d, err = time.Parse("2-Jan-2006 15:04:05 -0700", a.s)
			}
			if err != nil {
				return bad("invalid date-time")
			}
			date = d
		}
	}
	if int64(len(msg.s)) > s.srv.MaxMessage {
		return no("[TOOBIG] message too large")
	}
	uid, uv, err := s.acct.Append(name, message.NormalizeCRLF([]byte(msg.s)), flags, date)
	if errors.Is(err, store.ErrNoMailbox) {
		return no("[TRYCREATE] no such mailbox")
	}
	if err != nil {
		return no(err.Error())
	}
	return ok(fmt.Sprintf("[APPENDUID %d %d] APPEND completed", uv, uid))
}

// ---- selected-state commands ----

func (s *session) expunge(args []token, uid bool) result {
	if s.readOnly {
		return no("[READ-ONLY] mailbox is read-only")
	}
	var only []uint32
	if uid {
		if len(args) < 1 {
			return bad("UID EXPUNGE requires a UID set")
		}
		var err error
		if only, err = s.resolveUIDs(args[0], true); err != nil {
			return bad(err.Error())
		}
		if len(only) == 0 {
			return ok("EXPUNGE completed")
		}
	}
	if _, err := s.acct.Expunge(s.mailbox, only); err != nil {
		return no(err.Error())
	}
	return ok("EXPUNGE completed") // sync() emits the EXPUNGE responses
}

func (s *session) search(args []token, uid bool) result {
	if len(args) > 0 && strings.EqualFold(args[0].s, "RETURN") {
		return bad("ESEARCH not supported")
	}
	m, err := parseSearch(args)
	if err != nil {
		return bad(err.Error())
	}
	snap, err := s.acct.Snapshot(s.mailbox)
	if err != nil {
		return no(err.Error())
	}
	infos := map[uint32]*store.MessageInfo{}
	for _, mi := range snap.Messages {
		infos[mi.UID] = mi
	}
	var maxUID uint32
	if len(s.uids) > 0 {
		maxUID = s.uids[len(s.uids)-1]
	}
	var hits []string
	for i, u := range s.uids {
		info := infos[u]
		if info == nil {
			continue
		}
		ctx := &searchCtx{
			seq: uint32(i + 1), maxSeq: uint32(len(s.uids)), maxUID: maxUID, info: info,
			load: func() ([]byte, error) { return s.acct.Read(s.mailbox, u) },
		}
		if m(ctx) {
			if uid {
				hits = append(hits, fmt.Sprint(u))
			} else {
				hits = append(hits, fmt.Sprint(i+1))
			}
		}
	}
	if len(hits) == 0 {
		s.writef("* SEARCH\r\n")
	} else {
		s.writef("* SEARCH %s\r\n", strings.Join(hits, " "))
	}
	return ok("SEARCH completed")
}

func (s *session) fetch(args []token, uid bool) result {
	if len(args) < 2 {
		return bad("FETCH requires sequence set and items")
	}
	set, err := parseSeqSet(args[0].s)
	if err != nil {
		return bad("invalid sequence set")
	}
	items, err := parseFetchItems(args[1])
	if err != nil {
		return bad(err.Error())
	}
	hasUID, hasFlags, needMsg, marksSeen := false, false, false, false
	for _, it := range items {
		hasUID = hasUID || it.name == "UID"
		hasFlags = hasFlags || it.name == "FLAGS"
		needMsg = needMsg || it.needsMessage()
		marksSeen = marksSeen || (it.name == "BODYSECTION" && !it.peek)
	}
	if uid && !hasUID {
		items = append([]*fetchItem{{name: "UID"}}, items...)
	}

	idxs := s.resolve(set, uid)

	// Set \Seen in one batch before responding.
	seenSet := map[uint32]bool{}
	if marksSeen && !s.readOnly {
		snap, err := s.acct.Snapshot(s.mailbox)
		if err == nil {
			unseen := map[uint32]bool{}
			for _, m := range snap.Messages {
				if !m.HasFlag(store.FlagSeen) {
					unseen[m.UID] = true
				}
			}
			var toMark []uint32
			for _, i := range idxs {
				if unseen[s.uids[i]] {
					toMark = append(toMark, s.uids[i])
					seenSet[s.uids[i]] = true
				}
			}
			if len(toMark) > 0 {
				s.acct.SetFlags(s.mailbox, toMark, store.FlagsAdd, []string{store.FlagSeen})
			}
		}
	}

	snap, err := s.acct.Snapshot(s.mailbox)
	if err != nil {
		return no(err.Error())
	}
	infos := map[uint32]*store.MessageInfo{}
	for _, mi := range snap.Messages {
		infos[mi.UID] = mi
	}

	var b bytes.Buffer
	for _, i := range idxs {
		u := s.uids[i]
		info := infos[u]
		if info == nil {
			continue // expunged by another session; not yet reported
		}
		var raw []byte
		var part *message.Part
		if needMsg {
			if raw, err = s.acct.Read(s.mailbox, u); err != nil {
				continue
			}
			part = message.Parse(raw)
		}
		b.Reset()
		fmt.Fprintf(&b, "* %d FETCH (", i+1)
		first := true
		sep := func() {
			if !first {
				b.WriteByte(' ')
			}
			first = false
		}
		writeFlags := func() {
			sep()
			fmt.Fprintf(&b, "FLAGS (%s)", flagString(info.Flags))
			s.flags[u] = flagString(info.Flags)
		}
		for _, it := range items {
			switch it.name {
			case "UID":
				sep()
				fmt.Fprintf(&b, "UID %d", u)
			case "FLAGS":
				writeFlags()
			case "INTERNALDATE":
				sep()
				fmt.Fprintf(&b, "INTERNALDATE %s", quote(info.Date.Format(dateTimeLayout)))
			case "RFC822.SIZE":
				sep()
				fmt.Fprintf(&b, "RFC822.SIZE %d", info.Size)
			case "ENVELOPE":
				sep()
				b.WriteString("ENVELOPE ")
				writeEnvelope(&b, part.Header)
			case "BODYSTRUCTURE":
				sep()
				b.WriteString("BODYSTRUCTURE ")
				writeBodyStructure(&b, part, true)
			case "BODY":
				sep()
				b.WriteString("BODY ")
				writeBodyStructure(&b, part, false)
			case "BODYSECTION":
				sep()
				b.WriteString(it.respName)
				b.WriteByte(' ')
				b.WriteString(literalBytes(extractSection(part, raw, it)))
			}
		}
		if seenSet[u] && !hasFlags {
			writeFlags()
		}
		b.WriteString(")\r\n")
		s.w.Write(b.Bytes())
	}
	return ok("FETCH completed")
}

func (s *session) store(args []token, uid bool) result {
	if s.readOnly {
		return no("[READ-ONLY] mailbox is read-only")
	}
	if len(args) < 3 {
		return bad("STORE requires sequence set, item and flags")
	}
	uids, err := s.resolveUIDs(args[0], uid)
	if err != nil {
		return bad("invalid sequence set")
	}
	item := strings.ToUpper(args[1].s)
	silent := strings.HasSuffix(item, ".SILENT")
	item = strings.TrimSuffix(item, ".SILENT")
	var op store.FlagOp
	switch item {
	case "FLAGS":
		op = store.FlagsReplace
	case "+FLAGS":
		op = store.FlagsAdd
	case "-FLAGS":
		op = store.FlagsRemove
	default:
		return bad("invalid STORE item")
	}
	var flags []string
	for _, a := range args[2:] {
		if a.kind == tList {
			for _, f := range a.list {
				flags = append(flags, f.s)
			}
		} else {
			flags = append(flags, a.s)
		}
	}
	infos, err := s.acct.SetFlags(s.mailbox, uids, op, flags)
	if err != nil {
		return no(err.Error())
	}
	seqOf := map[uint32]int{}
	for i, u := range s.uids {
		seqOf[u] = i + 1
	}
	for _, info := range infos {
		f := flagString(info.Flags)
		s.flags[info.UID] = f
		if silent {
			continue
		}
		if uid {
			s.writef("* %d FETCH (UID %d FLAGS (%s))\r\n", seqOf[info.UID], info.UID, f)
		} else {
			s.writef("* %d FETCH (FLAGS (%s))\r\n", seqOf[info.UID], f)
		}
	}
	return ok("STORE completed")
}

func (s *session) copyMove(args []token, uid, move bool) result {
	if len(args) < 2 {
		return bad("missing arguments")
	}
	if move && s.readOnly {
		return no("[READ-ONLY] mailbox is read-only")
	}
	uids, err := s.resolveUIDs(args[0], uid)
	if err != nil {
		return bad("invalid sequence set")
	}
	dest := args[1].s
	if _, err := s.acct.Status(dest); errors.Is(err, store.ErrNoMailbox) {
		return no("[TRYCREATE] no such mailbox")
	}
	op := s.acct.Copy
	if move {
		op = s.acct.Move
	}
	src, dst, uv, err := op(s.mailbox, dest, uids)
	if err != nil {
		return no(err.Error())
	}
	code := ""
	if len(src) > 0 {
		code = fmt.Sprintf("[COPYUID %d %s %s] ", uv, formatUIDSetOrdered(src), formatUIDSetOrdered(dst))
	}
	if move {
		if code != "" {
			s.writef("* OK %smoved\r\n", code)
		}
		return ok("MOVE completed")
	}
	return ok(code + "COPY completed")
}

func (s *session) idle(tag string) bool {
	s.writef("+ idling\r\n")
	s.sync(true)
	s.w.Flush()
	lines := make(chan error, 1)
	go func() {
		s.conn.SetReadDeadline(time.Now().Add(idleTimeout))
		line, err := s.readLine()
		if err == nil && !strings.EqualFold(strings.TrimSpace(string(line)), "DONE") {
			err = errors.New("expected DONE")
		}
		lines <- err
	}()
	for {
		select {
		case <-s.watch:
			s.sync(true)
			if s.w.Flush() != nil {
				<-lines
				return true
			}
		case err := <-lines:
			if err != nil {
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					s.writef("* BYE autologout\r\n")
				} else {
					s.writef("%s BAD %s\r\n", tag, err)
				}
				return idleFatal(err)
			}
			s.writef("%s OK IDLE terminated\r\n", tag)
			return false
		}
	}
}

// idleFatal reports whether the IDLE reader error should end the connection.
func idleFatal(err error) bool {
	var nerr net.Error
	return errors.As(err, &nerr) || errors.Is(err, io.EOF)
}
