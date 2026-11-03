package webmail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"mailer/internal/message"
	"mailer/internal/mta"
	"mailer/internal/smtp"
	"mailer/internal/store"
)

// File is an attachment added in the browser.
type File struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Data string `json:"data"` // base64
}

// Ref names a stored message.
type Ref struct {
	Folder string `json:"folder"`
	UID    uint32 `json:"uid"`
}

// Draft is a message being written.
type Draft struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Cc      []string `json:"cc"`
	Bcc     []string `json:"bcc"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
	// The message this answers, for the reply headers.
	InReplyTo  string `json:"in_reply_to"`
	References string `json:"references"`
	Files      []File `json:"files"`
	// Keep attaches parts of a stored message again: a forward's
	// attachments, or a draft's.
	Keep *struct {
		Ref
		Parts []int `json:"parts"`
	} `json:"keep"`
	// Reply is the message answered or forwarded, marked so once sent.
	Reply   *Ref `json:"reply"`
	Forward bool `json:"forward"`
	// Replaces is the saved draft this one replaces, removed once it's sent
	// or saved again.
	Replaces *Ref `json:"replaces"`
}

type part struct {
	name, typ string
	data      []byte
}

// noBreaks strips what would end a header line early.
func noBreaks(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == 0 {
			return ' '
		}
		return r
	}, s)
}

// parseList reads the addresses the page typed, such as "Kris <kris@example.com>"
// or a bare address.
func parseList(in []string) ([]*mail.Address, error) {
	var out []*mail.Address
	for _, v := range in {
		v = strings.TrimSpace(noBreaks(v))
		if v == "" {
			continue
		}
		a, err := mail.ParseAddress(v)
		if err != nil {
			return nil, fmt.Errorf("%q isn't an email address", v)
		}
		a.Address = strings.ToLower(a.Address)
		out = append(out, a)
	}
	return out, nil
}

func joinAddrs(list []*mail.Address) string {
	s := make([]string, len(list))
	for i, a := range list {
		s[i] = a.String()
	}
	return strings.Join(s, ", ")
}

func boundary() string {
	b := make([]byte, 12)
	rand.Read(b)
	return "=_" + hex.EncodeToString(b)
}

func qp(text string) []byte {
	var b bytes.Buffer
	w := quotedprintable.NewWriter(&b)
	w.Write([]byte(strings.ReplaceAll(text, "\r\n", "\n")))
	w.Close()
	return b.Bytes()
}

func b64(data []byte) []byte {
	enc := base64.StdEncoding.EncodeToString(data)
	var b bytes.Buffer
	for len(enc) > 76 {
		b.WriteString(enc[:76] + "\r\n")
		enc = enc[76:]
	}
	b.WriteString(enc + "\r\n")
	return b.Bytes()
}

// build writes the message: the text and HTML as alternatives, then the
// attachments, with every line ending in CRLF.
func build(from *mail.Address, to, cc []*mail.Address, d *Draft, files []part, host string, draft bool) []byte {
	var b bytes.Buffer
	hdr := func(name, value string) {
		if value != "" {
			fmt.Fprintf(&b, "%s: %s\r\n", name, value)
		}
	}
	id := make([]byte, 12)
	rand.Read(id)
	hdr("From", from.String())
	hdr("To", joinAddrs(to))
	hdr("Cc", joinAddrs(cc))
	if draft {
		// Kept on the draft only, so it can be opened again with them.
		bcc, _ := parseList(d.Bcc)
		hdr("Bcc", joinAddrs(bcc))
	}
	hdr("Subject", mime.QEncoding.Encode("utf-8", noBreaks(d.Subject)))
	hdr("Date", time.Now().Format(time.RFC1123Z))
	hdr("Message-ID", fmt.Sprintf("<%s@%s>", hex.EncodeToString(id), host))
	hdr("In-Reply-To", noBreaks(d.InReplyTo))
	hdr("References", noBreaks(d.References))
	hdr("MIME-Version", "1.0")

	text := d.Text
	if text == "" {
		text = textOf(d.HTML)
	}
	alt := boundary()
	var body bytes.Buffer
	fmt.Fprintf(&body, "--%s\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", alt)
	body.Write(qp(text))
	fmt.Fprintf(&body, "\r\n--%s\r\nContent-Type: text/html; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", alt)
	body.Write(qp(`<!doctype html><html><head><meta charset="utf-8"></head><body>` + d.HTML + `</body></html>`))
	fmt.Fprintf(&body, "\r\n--%s--\r\n", alt)

	if len(files) == 0 {
		fmt.Fprintf(&b, "Content-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", alt)
		b.Write(body.Bytes())
		return b.Bytes()
	}
	mixed := boundary()
	fmt.Fprintf(&b, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", mixed)
	fmt.Fprintf(&b, "--%s\r\nContent-Type: multipart/alternative; boundary=\"%s\"\r\n\r\n", mixed, alt)
	b.Write(body.Bytes())
	for _, f := range files {
		typ := f.typ
		if _, _, err := mime.ParseMediaType(typ); err != nil || !strings.Contains(typ, "/") {
			typ = "application/octet-stream"
		}
		name := noBreaks(f.name)
		fmt.Fprintf(&b, "--%s\r\nContent-Type: %s\r\nContent-Transfer-Encoding: base64\r\nContent-Disposition: %s\r\n\r\n",
			mixed, mime.FormatMediaType(typ, map[string]string{"name": name}), contentDisposition("attachment", name))
		b.Write(b64(f.data))
	}
	fmt.Fprintf(&b, "--%s--\r\n", mixed)
	return b.Bytes()
}

// files gathers the draft's attachments: those added in the browser and
// those kept from a stored message.
func (s *Server) files(a *account, d *Draft) ([]part, error) {
	var out []part
	for _, f := range d.Files {
		data, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			return nil, fmt.Errorf("%s couldn't be read", f.Name)
		}
		out = append(out, part{clip(f.Name, 255), clip(f.Type, 100), data})
	}
	if d.Keep != nil && len(d.Keep.Parts) > 0 {
		raw, err := a.box.Read(d.Keep.Folder, d.Keep.UID)
		if err != nil {
			return nil, errors.New("the message these attachments came from is gone")
		}
		all := leaves(message.Parse(raw))
		for _, i := range d.Keep.Parts {
			if i < 0 || i >= len(all) {
				continue
			}
			p := all[i]
			_, name := disposition(p)
			if name == "" {
				name = defaultName(p)
			}
			out = append(out, part{name, p.Type + "/" + p.SubType, p.DecodedBody()})
		}
	}
	return out, nil
}

// compose checks a draft's addresses and builds it.
func (s *Server) compose(a *account, d *Draft, draft bool) (data []byte, from *mail.Address, rcpts []string, err error) {
	from = &mail.Address{Address: a.email}
	if d.From != "" {
		from.Address = strings.ToLower(strings.TrimSpace(d.From))
	}
	if !draft && !contains(s.addresses(a.email), from.Address) {
		return nil, nil, nil, errors.New("you can't send as " + from.Address)
	}
	from.Name = s.prefs.get(a.email).Name
	to, err := parseList(d.To)
	if err != nil {
		return nil, nil, nil, err
	}
	cc, err := parseList(d.Cc)
	if err != nil {
		return nil, nil, nil, err
	}
	bcc, err := parseList(d.Bcc)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, l := range [][]*mail.Address{to, cc, bcc} {
		for _, x := range l {
			if !contains(rcpts, x.Address) {
				rcpts = append(rcpts, x.Address)
			}
		}
	}
	if !draft && len(rcpts) == 0 {
		return nil, nil, nil, errors.New("add someone to send it to")
	}
	files, err := s.files(a, d)
	if err != nil {
		return nil, nil, nil, err
	}
	cfg := s.Cfg.Get()
	data = build(from, to, cc, d, files, cfg.Hostname, draft)
	if int64(len(data)) > cfg.MaxMessageBytes {
		return nil, nil, nil, fmt.Errorf("the message is %d MB with its attachments, over the %d MB limit", len(data)>>20, cfg.MaxMessageBytes>>20)
	}
	return data, from, rcpts, nil
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func (s *Server) readDraft(r *http.Request) (*Draft, error) {
	var d Draft
	// Attachments come base64 encoded, a third bigger than the message limit.
	limit := s.Cfg.Get().MaxMessageBytes*4/3 + 1<<20
	if err := readJSON(r, &d, limit); err != nil {
		return nil, errors.New("the message is too large, or couldn't be read")
	}
	return &d, nil
}

// send checks a message the way a mail app's would be on the submission
// port, delivers it, files a copy in Sent, and marks what it answered.
func (s *Server) send(w http.ResponseWriter, r *http.Request, a *account) {
	d, err := s.readDraft(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	data, from, rcpts, err := s.compose(a, d, false)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	sub := mta.Submission{MTA: s.MTA}
	env := &smtp.Envelope{AuthUser: a.email, From: from.Address, To: rcpts, Data: data, TLS: true}
	refused := func(err error) {
		var se *smtp.Error
		if errors.As(err, &se) {
			writeErr(w, 400, errors.New(sendText(se)))
			return
		}
		writeErr(w, 500, err)
	}
	if err := sub.CheckSender(env); err != nil {
		refused(err)
		return
	}
	for _, rc := range rcpts {
		if err := sub.CheckRecipient(env, rc); err != nil {
			var se *smtp.Error
			if errors.As(err, &se) {
				writeErr(w, 400, fmt.Errorf("%s: %s", rc, sendText(se)))
				return
			}
			refused(err)
			return
		}
	}
	if err := sub.Deliver(env); err != nil {
		refused(err)
		return
	}
	s.Log.Printf("webmail: %s sent a message to %d recipient(s)", a.email, len(rcpts))
	// Mail apps file their own copy unless the server is set to; the
	// webmail always gets one.
	if !s.Cfg.Get().SaveSentCopy {
		if sent := special(a, `\Sent`); sent != "" {
			a.box.Append(sent, data, []string{store.FlagSeen}, time.Now())
		}
	}
	if d.Reply != nil {
		flag := store.FlagAnswered
		if d.Forward {
			flag = flagForwarded
		}
		a.box.SetFlags(d.Reply.Folder, []uint32{d.Reply.UID}, store.FlagsAdd, []string{flag})
	}
	if d.Replaces != nil {
		s.removeDraft(a, d.Replaces)
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// sendText says why the server refused a message in words for the page.
func sendText(se *smtp.Error) string {
	switch {
	case strings.Contains(se.Msg, "locked"):
		return "Sending is turned off for this account."
	case strings.Contains(se.Msg, "not owned"):
		return "You can't send as that address."
	case strings.Contains(se.Msg, "no such user"):
		return "No such address here."
	case strings.Contains(se.Msg, "not accepting"):
		return "That mailbox isn't taking mail."
	}
	return se.Msg
}

// saveDraft files the message in Drafts, replacing the copy saved before.
func (s *Server) saveDraft(w http.ResponseWriter, r *http.Request, a *account) {
	d, err := s.readDraft(r)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	drafts := special(a, `\Drafts`)
	if drafts == "" {
		writeErr(w, 500, errors.New("this account has no Drafts folder"))
		return
	}
	data, _, _, err := s.compose(a, d, true)
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	uid, _, err := a.box.Append(drafts, data, []string{store.FlagDraft, store.FlagSeen}, time.Now())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if d.Replaces != nil {
		s.removeDraft(a, d.Replaces)
	}
	writeJSON(w, map[string]any{"folder": drafts, "uid": uid})
}

// removeDraft deletes a saved draft for good; only drafts are touched.
func (s *Server) removeDraft(a *account, ref *Ref) {
	_, m, err := s.find(a, ref.Folder, ref.UID)
	if err != nil || !m.HasFlag(store.FlagDraft) {
		return
	}
	s.expunge(a, ref.Folder, []uint32{ref.UID})
}
