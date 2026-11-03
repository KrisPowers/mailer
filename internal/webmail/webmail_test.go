package webmail

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mailer/internal/audit"
	"mailer/internal/config"
	"mailer/internal/mailauth"
	"mailer/internal/maillog"
	"mailer/internal/mta"
	"mailer/internal/store"
	"mailer/internal/users"
)

const password = "correct horse battery"

func newServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfgm, err := config.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfgm.Update(func(c *config.Config) {
		c.Domains = []string{"example.com"}
		c.Hostname = "mail.example.com"
		c.Aliases = map[string]string{"hello@example.com": "me@example.com"}
		c.SaveSentCopy = false
	})
	db, _ := users.Open(filepath.Join(dir, "users.json"))
	for _, u := range []string{"me@example.com", "you@example.com"} {
		if err := db.Add(u, password); err != nil {
			t.Fatal(err)
		}
	}
	st := store.New(filepath.Join(dir, "mail"))
	logger := log.New(io.Discard, "", 0)
	m := &mta.MTA{Cfg: cfgm, Users: db, Store: st, Log: logger, Mail: maillog.Open(filepath.Join(dir, "mail.log")),
		DKIM: func(string) *mailauth.Signer { return nil }}
	s := &Server{Cfg: cfgm, Users: db, Store: st, MTA: m, Limiter: users.NewLimiter(), Audit: audit.Open(filepath.Join(dir, "signins.log")),
		Log: logger, Dir: filepath.Join(dir, "webmail")}
	m.Rules = s.Sort
	return s
}

type client struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
}

func (c *client) do(method, path string, body any) (int, map[string]any, *httptest.ResponseRecorder) {
	c.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, "http://localhost:8081"+path, r)
	req.Header.Set("X-Mailer", "1")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rr := httptest.NewRecorder()
	c.h.ServeHTTP(rr, req)
	for _, ck := range rr.Result().Cookies() {
		if ck.Name == cookieName {
			c.cookie = ck
		}
	}
	out := map[string]any{}
	json.Unmarshal(rr.Body.Bytes(), &out)
	return rr.Code, out, rr
}

func signIn(t *testing.T, s *Server, email string) *client {
	t.Helper()
	c := &client{t: t, h: s.Handler()}
	if code, out, _ := c.do("POST", "/api/login", map[string]string{"email": email, "password": password}); code != 200 {
		t.Fatalf("sign-in: %d %v", code, out)
	}
	return c
}

func TestSignIn(t *testing.T) {
	s := newServer(t)
	c := &client{t: t, h: s.Handler()}
	if code, _, _ := c.do("GET", "/api/folders", nil); code != 401 {
		t.Errorf("folders without a session: %d", code)
	}
	if code, _, _ := c.do("POST", "/api/login", map[string]string{"email": "me@example.com", "password": "wrong password"}); code != 401 {
		t.Errorf("wrong password: %d", code)
	}
	c = signIn(t, s, "Me@Example.com")
	code, _, rr := c.do("GET", "/api/folders", nil)
	var folders []Folder
	json.Unmarshal(rr.Body.Bytes(), &folders)
	if code != 200 || len(folders) != 6 || folders[0].Special != "inbox" || folders[1].Special != "drafts" {
		t.Errorf("folders: %d %+v", code, folders)
	}

	// The webmail works for an account whose mail apps are off.
	if s.Users.MailApps("me@example.com") {
		t.Error("a new account's mail apps are on")
	}

	// A new password ends the session.
	s.Users.SetPassword("me@example.com", "another long password")
	if code, _, _ := c.do("GET", "/api/folders", nil); code != 401 {
		t.Errorf("session outlived the password: %d", code)
	}

	// Another site can't post without the header, nor reach it by another name.
	req := httptest.NewRequest("POST", "http://localhost:8081/api/logout", nil)
	rr2 := httptest.NewRecorder()
	s.Handler().ServeHTTP(rr2, req)
	if rr2.Code != 403 {
		t.Errorf("post without X-Mailer: %d", rr2.Code)
	}
	req = httptest.NewRequest("GET", "http://evil.example/api/session", nil)
	rr2 = httptest.NewRecorder()
	s.Handler().ServeHTTP(rr2, req)
	if rr2.Code != 403 {
		t.Errorf("foreign host: %d", rr2.Code)
	}
}

func TestSendAndRead(t *testing.T) {
	s := newServer(t)
	me := signIn(t, s, "me@example.com")
	code, out, _ := me.do("POST", "/api/send", map[string]any{
		"from": "hello@example.com", "to": []string{"You <you@example.com>"}, "subject": "Lunch, café?",
		"html": "<p>Noon <b>today</b>?</p>", "text": "Noon today?",
		"files": []map[string]string{{"name": "menu.txt", "type": "text/plain", "data": "cGFzdGE="}},
	})
	if code != 200 {
		t.Fatalf("send: %d %v", code, out)
	}
	if code, _, _ := me.do("POST", "/api/send", map[string]any{"from": "you@example.com", "to": []string{"you@example.com"}, "html": "x"}); code != 400 {
		t.Errorf("sent as someone else: %d", code)
	}

	// The copy in Sent, filed by the webmail.
	_, list, _ := me.do("GET", "/api/messages?folder=Sent", nil)
	if list["total"] != float64(1) {
		t.Errorf("sent copies: %v", list)
	}

	you := signIn(t, s, "you@example.com")
	_, list, rr := you.do("GET", "/api/messages?folder=INBOX", nil)
	var page struct {
		Total    int       `json:"total"`
		Messages []Summary `json:"messages"`
	}
	json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Total != 1 || page.Messages[0].Subject != "Lunch, café?" || page.Messages[0].From.Email != "hello@example.com" ||
		!page.Messages[0].Attachments || page.Messages[0].Seen {
		t.Fatalf("inbox: %v", list)
	}
	uid := page.Messages[0].UID

	_, _, rr = you.do("GET", "/api/message?folder=INBOX&uid="+itoa(uid), nil)
	var d Detail
	json.Unmarshal(rr.Body.Bytes(), &d)
	if d.Text != "Noon today?" || !d.HTML || len(d.Files) != 1 || d.Files[0].Name != "menu.txt" {
		t.Errorf("message: %+v", d)
	}
	_, _, rr = you.do("GET", "/api/message/part?folder=INBOX&uid="+itoa(uid)+"&index="+itoa(uint32(d.Files[0].Index)), nil)
	if rr.Body.String() != "pasta" || !strings.HasPrefix(rr.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("attachment: %q %q", rr.Body.String(), rr.Header().Get("Content-Disposition"))
	}

	// The HTML is its own sandboxed page, with no pictures from the internet.
	_, _, rr = you.do("GET", "/api/message/body?folder=INBOX&uid="+itoa(uid), nil)
	csp := rr.Header().Get("Content-Security-Policy")
	if !strings.Contains(rr.Body.String(), "<b>today</b>") || !strings.Contains(csp, "sandbox") || strings.Contains(csp, "https:") ||
		strings.Contains(csp, "allow-scripts") || rr.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Errorf("body: %q %q", csp, rr.Body.String())
	}

	// Read, a category, then flagged-only shows nothing.
	if code, out, _ := you.do("POST", "/api/messages/flags", map[string]any{"folder": "INBOX", "uids": []uint32{uid}, "add": []string{"seen", "cat:red"}}); code != 200 {
		t.Fatalf("flags: %d %v", code, out)
	}
	_, _, rr = you.do("GET", "/api/messages?folder=INBOX&show=cat:red", nil)
	json.Unmarshal(rr.Body.Bytes(), &page)
	if page.Total != 1 || !page.Messages[0].Seen || len(page.Messages[0].Categories) != 1 {
		t.Errorf("category: %+v", page)
	}
	if _, l, _ := you.do("GET", "/api/messages?folder=INBOX&show=flagged", nil); l["total"] != float64(0) {
		t.Errorf("flagged: %v", l)
	}
	if _, l, _ := you.do("GET", "/api/messages?folder=INBOX&q=noon", nil); l["total"] != float64(1) {
		t.Errorf("search: %v", l)
	}

	// Delete goes to Trash, and from Trash for good.
	you.do("POST", "/api/messages/delete", map[string]any{"folder": "INBOX", "uids": []uint32{uid}})
	_, l, rr := you.do("GET", "/api/messages?folder=Trash", nil)
	json.Unmarshal(rr.Body.Bytes(), &page)
	if l["total"] != float64(1) {
		t.Fatalf("trash: %v", l)
	}
	you.do("POST", "/api/messages/delete", map[string]any{"folder": "Trash", "uids": []uint32{page.Messages[0].UID}})
	if _, l, _ := you.do("GET", "/api/messages?folder=Trash", nil); l["total"] != float64(0) {
		t.Errorf("trash after deleting: %v", l)
	}
}

func TestDrafts(t *testing.T) {
	s := newServer(t)
	me := signIn(t, s, "me@example.com")
	code, out, _ := me.do("POST", "/api/drafts", map[string]any{"to": []string{"you@example.com"}, "bcc": []string{"hidden@example.net"}, "subject": "Plan", "html": "<p>first</p>"})
	if code != 200 {
		t.Fatalf("draft: %d %v", code, out)
	}
	first := out["uid"].(float64)
	_, out, _ = me.do("POST", "/api/drafts", map[string]any{"subject": "Plan", "html": "<p>second</p>", "replaces": map[string]any{"folder": "Drafts", "uid": first}})
	_, l, _ := me.do("GET", "/api/messages?folder=Drafts", nil)
	if l["total"] != float64(1) {
		t.Errorf("drafts after saving again: %v", l)
	}
	_, _, rr := me.do("GET", "/api/message?folder=Drafts&edit=1&uid="+itoa(uint32(out["uid"].(float64))), nil)
	var d Detail
	json.Unmarshal(rr.Body.Bytes(), &d)
	if !d.Draft || !strings.Contains(d.Source, "second") {
		t.Errorf("draft: %+v", d)
	}
}

func TestRules(t *testing.T) {
	s := newServer(t)
	me := signIn(t, s, "me@example.com")
	if code, _, _ := me.do("POST", "/api/folders", map[string]string{"name": "News"}); code != 200 {
		t.Fatal("create folder")
	}
	prefs := s.prefs.get("me@example.com")
	prefs.Rules = []Rule{{ID: "r1", Name: "Newsletters", On: true, Conditions: []Condition{{Field: "subject", Op: "contains", Value: "weekly"}},
		Actions: []Action{{Kind: "move", Value: "News"}, {Kind: "category", Value: "blue"}}, Stop: true}}
	if code, out, _ := me.do("PUT", "/api/prefs", prefs); code != 200 {
		t.Fatalf("prefs: %d %v", code, out)
	}
	bad := prefs
	bad.Rules = []Rule{{ID: "r2", Name: "Nothing", On: true, Actions: []Action{{Kind: "read"}}}}
	if code, _, _ := me.do("PUT", "/api/prefs", bad); code != 400 {
		t.Errorf("a rule without conditions saved: %d", code)
	}

	// Mail from another account here is filed on delivery.
	you := signIn(t, s, "you@example.com")
	if code, out, _ := you.do("POST", "/api/send", map[string]any{"to": []string{"me@example.com"}, "subject": "The Weekly", "html": "News."}); code != 200 {
		t.Fatalf("send: %d %v", code, out)
	}
	box, _ := s.Store.Account("me@example.com")
	news, _ := box.Snapshot("News")
	inbox, _ := box.Snapshot("INBOX")
	if len(news.Messages) != 1 || len(inbox.Messages) != 0 || !news.Messages[0].HasFlag(catFlag("blue")) {
		t.Errorf("filed: news %d, inbox %d", len(news.Messages), len(inbox.Messages))
	}

	// Renaming the folder follows into the rule.
	me.do("POST", "/api/folders/rename", map[string]string{"from": "News", "to": "Reading"})
	if got := s.prefs.get("me@example.com").Rules[0].Actions[0].Value; got != "Reading" {
		t.Errorf("rule after rename: %q", got)
	}

	// Running the rules now files what's already there.
	raw := []byte("From: Paper <paper@example.net>\r\nTo: me@example.com\r\nSubject: The Weekly\r\n\r\nNews.\r\n")
	box.Append("INBOX", raw, nil, time.Now())
	if _, out, _ := me.do("POST", "/api/rules/run", map[string]string{"folder": "INBOX"}); out["matched"] != float64(1) {
		t.Errorf("run: %v", out)
	}
	if inbox, _ := box.Snapshot("INBOX"); len(inbox.Messages) != 0 {
		t.Error("run left the message in INBOX")
	}
}

func TestBuild(t *testing.T) {
	d := &Draft{Subject: "Hi\r\nBcc: evil@example.net", HTML: "<p>Hi</p>", Bcc: []string{"secret@example.net"}}
	from, _ := parseList([]string{"Kris <kris@example.com>"})
	raw := string(build(from[0], from, nil, d, nil, "mail.example.com", false))
	if strings.Contains(raw, "\r\nBcc:") || strings.Contains(raw, "secret@") {
		t.Errorf("header injected or Bcc leaked:\n%s", raw)
	}
	p := parse([]byte(raw))
	if p.Subject != "Hi  Bcc: evil@example.net" || p.Text != "Hi" || !p.HasHTML {
		t.Errorf("parsed back: %+v", p)
	}
}

func itoa(n uint32) string { return strconv.FormatUint(uint64(n), 10) }
