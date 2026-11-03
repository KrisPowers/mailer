package webmail

import (
	"encoding/base64"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"mailer/internal/message"
	"mailer/internal/store"
)

// Folder is a mailbox as the folder list shows it.
type Folder struct {
	Name    string `json:"name"`
	Special string `json:"special,omitempty"` // inbox, sent, drafts, trash, junk or archive
	Total   int    `json:"total"`
	Unread  int    `json:"unread"`
	Size    int64  `json:"size"`
}

var specialNames = map[string]string{`\Sent`: "sent", `\Drafts`: "drafts", `\Trash`: "trash", `\Junk`: "junk", `\Archive`: "archive"}

// The order the special folders come in, before the rest by name.
var specialOrder = map[string]int{"inbox": 0, "drafts": 1, "sent": 2, "archive": 3, "junk": 4, "trash": 5}

func (s *Server) folderList(a *account) []Folder {
	var out []Folder
	for _, mb := range a.box.List() {
		st, err := a.box.Status(mb.Name)
		if err != nil {
			continue
		}
		f := Folder{Name: mb.Name, Special: specialNames[mb.SpecialUse], Total: st.Messages, Unread: st.Unseen, Size: st.Size}
		if mb.Name == "INBOX" {
			f.Special = "inbox"
		}
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool {
		oi, iok := specialOrder[out[i].Special]
		oj, jok := specialOrder[out[j].Special]
		switch {
		case iok && jok:
			return oi < oj
		case iok != jok:
			return iok
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (s *Server) folders(w http.ResponseWriter, r *http.Request, a *account) {
	writeJSON(w, s.folderList(a))
}

// special is the name of the account's folder with the attribute, such as
// `\Trash`, or "" when it has none.
func special(a *account, attr string) string { return a.box.FindSpecialUse(attr) }

func (s *Server) isSpecial(a *account, name string) bool {
	if strings.EqualFold(name, "INBOX") {
		return true
	}
	for attr := range specialNames {
		if special(a, attr) == name {
			return true
		}
	}
	return false
}

func folderErr(err error) error {
	switch {
	case errors.Is(err, store.ErrExists):
		return errors.New("there's already a folder by that name")
	case errors.Is(err, store.ErrBadName):
		return errors.New("folder names can't be empty, start or end with /, or contain * or %")
	case errors.Is(err, store.ErrNoMailbox):
		return errors.New("no such folder")
	}
	return err
}

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request, a *account) {
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req, 1<<12); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := a.box.Create(strings.TrimSpace(req.Name)); err != nil {
		writeErr(w, 400, folderErr(err))
		return
	}
	writeJSON(w, s.folderList(a))
}

// renameFolder renames a folder and its subfolders, and the rules that
// file into them.
func (s *Server) renameFolder(w http.ResponseWriter, r *http.Request, a *account) {
	var req struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := readJSON(r, &req, 1<<12); err != nil {
		writeErr(w, 400, err)
		return
	}
	req.To = strings.TrimSpace(req.To)
	if s.isSpecial(a, req.From) {
		writeErr(w, 400, errors.New("this folder keeps its name"))
		return
	}
	if err := a.box.Rename(req.From, req.To); err != nil {
		writeErr(w, 400, folderErr(err))
		return
	}
	s.prefs.update(a.email, func(p *Prefs) {
		for i := range p.Rules {
			for j := range p.Rules[i].Actions {
				act := &p.Rules[i].Actions[j]
				if (act.Kind == "move" || act.Kind == "copy") && (act.Value == req.From || strings.HasPrefix(act.Value, req.From+store.Delimiter)) {
					act.Value = req.To + strings.TrimPrefix(act.Value, req.From)
				}
			}
		}
	})
	writeJSON(w, s.folderList(a))
}

// deleteFolder moves a folder's messages to Trash, then removes it.
func (s *Server) deleteFolder(w http.ResponseWriter, r *http.Request, a *account) {
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req, 1<<12); err != nil {
		writeErr(w, 400, err)
		return
	}
	if s.isSpecial(a, req.Name) {
		writeErr(w, 400, errors.New("this folder can't be removed"))
		return
	}
	if trash := special(a, `\Trash`); trash != "" {
		if snap, err := a.box.Snapshot(req.Name); err == nil && len(snap.Messages) > 0 {
			if _, _, _, err := a.box.Move(req.Name, trash, uidsOf(snap.Messages)); err != nil {
				writeErr(w, 500, err)
				return
			}
		}
	}
	if err := a.box.Delete(req.Name); err != nil {
		writeErr(w, 400, folderErr(err))
		return
	}
	writeJSON(w, s.folderList(a))
}

// emptyFolder removes every message in Trash or Junk for good.
func (s *Server) emptyFolder(w http.ResponseWriter, r *http.Request, a *account) {
	var req struct {
		Name string `json:"name"`
	}
	if err := readJSON(r, &req, 1<<12); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.Name != special(a, `\Trash`) && req.Name != special(a, `\Junk`) {
		writeErr(w, 400, errors.New("only Trash and Junk can be emptied"))
		return
	}
	if err := s.expunge(a, req.Name, nil); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, s.folderList(a))
}

// expunge removes uids from folder for good; nil means every message.
func (s *Server) expunge(a *account, folder string, uids []uint32) error {
	if uids == nil {
		snap, err := a.box.Snapshot(folder)
		if err != nil {
			return err
		}
		uids = uidsOf(snap.Messages)
	}
	if len(uids) == 0 {
		return nil
	}
	if _, err := a.box.SetFlags(folder, uids, store.FlagsAdd, []string{store.FlagDeleted}); err != nil {
		return err
	}
	_, err := a.box.Expunge(folder, uids)
	return err
}

func uidsOf(ms []*store.MessageInfo) []uint32 {
	out := make([]uint32, len(ms))
	for i, m := range ms {
		out[i] = m.UID
	}
	return out
}

// Summary is a message as a list shows it.
type Summary struct {
	Folder      string    `json:"folder"`
	UID         uint32    `json:"uid"`
	From        Addr      `json:"from"`
	To          []Addr    `json:"to"`
	Subject     string    `json:"subject"`
	Date        time.Time `json:"date"`
	Snippet     string    `json:"snippet"`
	Size        int64     `json:"size"`
	Seen        bool      `json:"seen"`
	Flagged     bool      `json:"flagged"`
	Answered    bool      `json:"answered"`
	Forwarded   bool      `json:"forwarded"`
	Draft       bool      `json:"draft"`
	Categories  []string  `json:"categories"`
	Attachments bool      `json:"attachments"`
	Risky       bool      `json:"risky"`
	Marketing   bool      `json:"marketing"`
}

const flagForwarded = "$Forwarded"

func (s *Server) parsedOf(a *account, folder string, uidValidity uint32, m *store.MessageInfo) *parsed {
	key := fmt.Sprintf("%s\x00%s\x00%d\x00%d", a.email, folder, uidValidity, m.UID)
	return s.cache.get(key, func() ([]byte, error) { return a.box.Read(folder, m.UID) })
}

func summaryOf(folder string, m *store.MessageInfo, p *parsed) Summary {
	sum := Summary{Folder: folder, UID: m.UID, From: p.From, To: p.To, Subject: p.Subject, Date: m.Date, Snippet: p.Snippet,
		Size: m.Size, Seen: m.HasFlag(store.FlagSeen), Flagged: m.HasFlag(store.FlagFlagged), Answered: m.HasFlag(store.FlagAnswered),
		Forwarded: m.HasFlag(flagForwarded), Draft: m.HasFlag(store.FlagDraft), Categories: []string{},
		Risky: m.HasFlag(store.FlagRisky), Marketing: m.HasFlag(store.FlagMarketing) || p.Marketing}
	if sum.To == nil {
		sum.To = []Addr{}
	}
	for _, f := range m.Flags {
		if id, ok := strings.CutPrefix(f, catPrefix); ok {
			sum.Categories = append(sum.Categories, id)
		}
	}
	for _, at := range p.Attachments {
		if !at.Inline {
			sum.Attachments = true
		}
	}
	return sum
}

// matches reports whether a message has every word of q in who it's from or
// to, its subject or its text.
func matches(p *parsed, words []string) bool {
	if len(words) == 0 {
		return true
	}
	var b strings.Builder
	b.WriteString(strings.ToLower(p.From.Name + " " + p.From.Email + " " + p.Subject + " "))
	for _, to := range append(append([]Addr{}, p.To...), p.Cc...) {
		b.WriteString(strings.ToLower(to.Name + " " + to.Email + " "))
	}
	b.WriteString(strings.ToLower(p.Text))
	hay := b.String()
	for _, w := range words {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// messages lists a folder newest first, a page at a time: with q, only those
// that match, and with all=1, from every folder. show narrows it to unread,
// flagged, with attachments, or one category (cat:<id>).
func (s *Server) messages(w http.ResponseWriter, r *http.Request, a *account) {
	q := r.URL.Query()
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	offset = max(offset, 0)
	words := strings.Fields(strings.ToLower(q.Get("q")))
	show := q.Get("show")
	folders := []string{q.Get("folder")}
	if q.Get("all") == "1" {
		folders = folders[:0]
		for _, f := range s.folderList(a) {
			folders = append(folders, f.Name)
		}
	}

	type item struct {
		folder string
		uv     uint32
		m      *store.MessageInfo
	}
	var items []item
	for _, folder := range folders {
		snap, err := a.box.Snapshot(folder)
		if err != nil {
			if len(folders) == 1 {
				writeErr(w, 404, folderErr(err))
				return
			}
			continue
		}
		for _, m := range snap.Messages {
			if m.HasFlag(store.FlagDeleted) {
				continue
			}
			switch {
			case show == "unread" && m.HasFlag(store.FlagSeen),
				show == "flagged" && !m.HasFlag(store.FlagFlagged),
				strings.HasPrefix(show, "cat:") && !m.HasFlag(catFlag(strings.TrimPrefix(show, "cat:"))):
				continue
			}
			items = append(items, item{snap.Name, snap.UIDValidity, m})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].m.Date.After(items[j].m.Date) })

	// Narrowing by text or attachments needs each message read; the cache
	// makes that quick after the first time.
	if len(words) > 0 || show == "attachments" {
		kept := items[:0]
		for _, it := range items {
			p := s.parsedOf(a, it.folder, it.uv, it.m)
			if matches(p, words) && (show != "attachments" || slices.ContainsFunc(p.Attachments, func(x Attachment) bool { return !x.Inline })) {
				kept = append(kept, it)
			}
		}
		items = kept
	}
	total := len(items)
	page := items[min(offset, total):min(offset+limit, total)]
	out := make([]Summary, 0, len(page))
	for _, it := range page {
		out = append(out, summaryOf(it.folder, it.m, s.parsedOf(a, it.folder, it.uv, it.m)))
	}
	writeJSON(w, map[string]any{"total": total, "messages": out})
}

// find returns the message folder and uid name, as the list has it.
func (s *Server) find(a *account, folder string, uid uint32) (*store.Snapshot, *store.MessageInfo, error) {
	snap, err := a.box.Snapshot(folder)
	if err != nil {
		return nil, nil, folderErr(err)
	}
	for _, m := range snap.Messages {
		if m.UID == uid {
			return snap, m, nil
		}
	}
	return nil, nil, errors.New("that message is gone")
}

func target(r *http.Request) (string, uint32) {
	uid, _ := strconv.ParseUint(r.URL.Query().Get("uid"), 10, 32)
	return r.URL.Query().Get("folder"), uint32(uid)
}

// Detail is one message to read.
type Detail struct {
	Summary
	Cc          []Addr       `json:"cc"`
	Bcc         []Addr       `json:"bcc"`
	ReplyTo     []Addr       `json:"reply_to"`
	MessageID   string       `json:"message_id"`
	InReplyTo   string       `json:"in_reply_to"`
	References  string       `json:"references"`
	Sent        time.Time    `json:"sent"`
	Text        string       `json:"text"`
	HTML        bool         `json:"html"`
	Remote      bool         `json:"remote"`
	Files       []Attachment `json:"files"`
	Auth        []AuthResult `json:"auth"`
	Junk        string       `json:"junk,omitempty"`
	// Source is the HTML itself, sent only for writing a reply, a forward
	// or a draft, for the page to clean before it edits it.
	Source string `json:"source,omitempty"`
}

// AuthResult is how a sender check went when the message arrived.
type AuthResult struct {
	Check  string `json:"check"`  // spf, dkim or dmarc
	Result string `json:"result"` // pass, fail, none and the like
}

var authRe = regexp.MustCompile(`(?i)\b(spf|dkim|dmarc)=([a-z]+)`)

func (s *Server) message(w http.ResponseWriter, r *http.Request, a *account) {
	folder, uid := target(r)
	snap, m, err := s.find(a, folder, uid)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	p := s.parsedOf(a, snap.Name, snap.UIDValidity, m)
	d := Detail{Summary: summaryOf(snap.Name, m, p), Cc: p.Cc, Bcc: p.Bcc, ReplyTo: p.ReplyTo, MessageID: p.MessageID,
		InReplyTo: p.InReplyTo, References: p.References, Sent: p.Sent, Text: p.Text, HTML: p.HasHTML, Remote: p.Remote, Files: []Attachment{}, Auth: []AuthResult{}}
	for _, at := range p.Attachments {
		if !at.Inline {
			d.Files = append(d.Files, at)
		}
	}
	raw, err := a.box.Read(snap.Name, uid)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	hdr := headerOf(raw)
	host := s.Cfg.Get().Hostname
	for _, f := range hdr {
		switch {
		case strings.EqualFold(f.Name, "Authentication-Results") && len(d.Auth) == 0:
			id, rest, _ := strings.Cut(f.Value, ";")
			if !strings.EqualFold(strings.TrimSpace(id), host) {
				continue
			}
			for _, m := range authRe.FindAllStringSubmatch(rest, -1) {
				d.Auth = append(d.Auth, AuthResult{strings.ToLower(m[1]), strings.ToLower(m[2])})
			}
		case strings.EqualFold(f.Name, "X-Mailer-Junk"):
			d.Junk = strings.TrimSpace(strings.TrimPrefix(f.Value, "yes"))
			d.Junk = strings.Trim(d.Junk, " ()")
		}
	}
	if r.URL.Query().Get("edit") == "1" {
		d.Source = htmlBody(raw)
	}
	writeJSON(w, d)
}

// body is the message's HTML as a page of its own, for a sandboxed frame in
// the reading pane: no scripts, no forms, no way out but links opening in a
// new tab, and pictures from the internet only when asked for.
func (s *Server) body(w http.ResponseWriter, r *http.Request, a *account) {
	folder, uid := target(r)
	if _, _, err := s.find(a, folder, uid); err != nil {
		writeErr(w, 404, err)
		return
	}
	raw, err := a.box.Read(folder, uid)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	img := "data:"
	if r.URL.Query().Get("images") == "1" {
		img = "data: https: http:"
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; font-src data:; img-src "+img+
		"; frame-ancestors 'self'; form-action 'none'; sandbox allow-popups allow-popups-to-escape-sandbox")
	body := refreshRe.ReplaceAllString(htmlBody(raw), "")
	fmt.Fprint(w, `<!doctype html><html><head><meta charset="utf-8"><meta name="referrer" content="no-referrer"><base target="_blank">`+
		`<style>html{background:#fff}body{margin:0;padding:20px 24px;color:#1f1f1f;font:14px/1.5 Roboto,"Segoe UI",system-ui,sans-serif;overflow-wrap:break-word}`+
		`img{max-width:100%;height:auto}table{max-width:100%}pre{white-space:pre-wrap}</style></head><body>`)
	fmt.Fprint(w, body)
	fmt.Fprint(w, `</body></html>`)
}

var refreshRe = regexp.MustCompile(`(?i)<meta[^>]+http-equiv\s*=\s*["']?refresh[^>]*>`)

// part downloads one of a message's attachments; a picture can open in the
// browser instead, with inline=1.
func (s *Server) part(w http.ResponseWriter, r *http.Request, a *account) {
	folder, uid := target(r)
	if _, _, err := s.find(a, folder, uid); err != nil {
		writeErr(w, 404, err)
		return
	}
	raw, err := a.box.Read(folder, uid)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	index, _ := strconv.Atoi(r.URL.Query().Get("index"))
	all := leaves(message.Parse(raw))
	if index < 0 || index >= len(all) {
		writeErr(w, 404, errors.New("no such attachment"))
		return
	}
	p := all[index]
	_, name := disposition(p)
	if name == "" {
		name = defaultName(p)
	}
	typ := p.Type + "/" + p.SubType
	disp := "attachment"
	if r.URL.Query().Get("inline") == "1" && p.Type == "image" && p.SubType != "svg+xml" {
		disp = "inline"
	} else {
		typ = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("Content-Disposition", contentDisposition(disp, name))
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; img-src data:")
	h.Set("Cache-Control", "private, no-store")
	w.Write(p.DecodedBody())
}

// raw downloads the whole message as it was stored, as a .eml file.
func (s *Server) raw(w http.ResponseWriter, r *http.Request, a *account) {
	folder, uid := target(r)
	if _, _, err := s.find(a, folder, uid); err != nil {
		writeErr(w, 404, err)
		return
	}
	raw, err := a.box.Read(folder, uid)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", contentDisposition("attachment", "message.eml"))
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Write(raw)
}

func headerOf(raw []byte) message.Header {
	h, _ := message.SplitHeader(raw)
	return message.ParseHeader(h)
}

// contentDisposition names a download, encoding a name that isn't plain ASCII.
func contentDisposition(disp, name string) string {
	if v := mime.FormatMediaType(disp, map[string]string{"filename": name}); v != "" {
		return v
	}
	return disp
}

func dataURI(typ string, b []byte) string {
	return "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(b)
}

// flagNames are the flags the page may set or clear.
var flagNames = map[string]string{"seen": store.FlagSeen, "flagged": store.FlagFlagged, "answered": store.FlagAnswered, "forwarded": flagForwarded}

func flagOf(name string) (string, error) {
	if f, ok := flagNames[name]; ok {
		return f, nil
	}
	if id, ok := strings.CutPrefix(name, "cat:"); ok && idRe.MatchString(id) {
		return catFlag(id), nil
	}
	return "", fmt.Errorf("unknown flag %q", name)
}

type selection struct {
	Folder string   `json:"folder"`
	UIDs   []uint32 `json:"uids"`
}

// setFlags marks messages read or unread, flags them, or adds and removes
// categories.
func (s *Server) setFlags(w http.ResponseWriter, r *http.Request, a *account) {
	var req struct {
		selection
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := readJSON(r, &req, 1<<20); err != nil {
		writeErr(w, 400, err)
		return
	}
	for _, set := range []struct {
		names []string
		op    store.FlagOp
	}{{req.Add, store.FlagsAdd}, {req.Remove, store.FlagsRemove}} {
		var flags []string
		for _, n := range set.names {
			f, err := flagOf(n)
			if err != nil {
				writeErr(w, 400, err)
				return
			}
			flags = append(flags, f)
		}
		if len(flags) == 0 {
			continue
		}
		if _, err := a.box.SetFlags(req.Folder, req.UIDs, set.op, flags); err != nil {
			writeErr(w, 400, folderErr(err))
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) move(w http.ResponseWriter, r *http.Request, a *account) {
	var req struct {
		selection
		To string `json:"to"`
	}
	if err := readJSON(r, &req, 1<<20); err != nil {
		writeErr(w, 400, err)
		return
	}
	if req.To == req.Folder {
		writeJSON(w, map[string]bool{"ok": true})
		return
	}
	if _, _, _, err := a.box.Move(req.Folder, req.To, req.UIDs); err != nil {
		writeErr(w, 400, folderErr(err))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// deleteMessages moves messages to Trash, or removes them for good when
// they're in Trash already.
func (s *Server) deleteMessages(w http.ResponseWriter, r *http.Request, a *account) {
	var req selection
	if err := readJSON(r, &req, 1<<20); err != nil {
		writeErr(w, 400, err)
		return
	}
	trash := special(a, `\Trash`)
	var err error
	if trash == "" || req.Folder == trash {
		err = s.expunge(a, req.Folder, req.UIDs)
	} else {
		_, _, _, err = a.box.Move(req.Folder, trash, req.UIDs)
	}
	if err != nil {
		writeErr(w, 400, folderErr(err))
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// contacts are the people the account writes to and hears from most, for
// filling in addresses.
func (s *Server) contacts(w http.ResponseWriter, r *http.Request, a *account) {
	count := map[string]int{}
	names := map[string]string{}
	add := func(x Addr, weight int) {
		if x.Email == "" || x.Email == a.email {
			return
		}
		count[x.Email] += weight
		if x.Name != "" {
			names[x.Email] = x.Name
		}
	}
	for _, spec := range []struct {
		attr   string
		weight int
	}{{`\Sent`, 3}, {"INBOX", 1}} {
		folder := spec.attr
		if folder != "INBOX" {
			folder = special(a, spec.attr)
		}
		snap, err := a.box.Snapshot(folder)
		if err != nil {
			continue
		}
		ms := snap.Messages
		if len(ms) > 500 {
			ms = ms[len(ms)-500:]
		}
		for _, m := range ms {
			p := s.parsedOf(a, snap.Name, snap.UIDValidity, m)
			if spec.weight == 1 {
				add(p.From, 1)
				continue
			}
			for _, to := range append(append([]Addr{}, p.To...), p.Cc...) {
				add(to, spec.weight)
			}
		}
	}
	out := make([]Addr, 0, len(count))
	for e := range count {
		out = append(out, Addr{Name: names[e], Email: e})
	}
	sort.Slice(out, func(i, j int) bool {
		if count[out[i].Email] != count[out[j].Email] {
			return count[out[i].Email] > count[out[j].Email]
		}
		return out[i].Email < out[j].Email
	})
	if len(out) > 500 {
		out = out[:500]
	}
	writeJSON(w, out)
}
