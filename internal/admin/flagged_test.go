package admin

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"mailer/internal/store"
)

func TestFlagged(t *testing.T) {
	s := newServer(t)
	h := s.Handler()
	hdr := signInAdmin(t, s)
	if err := s.Users.Add("me@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	acct, _ := s.Store.Account("me@example.com")
	msg := func(subject string) []byte {
		return []byte("From: a@example.net\r\nSubject: " + subject + "\r\n\r\nhi\r\n")
	}
	acct.Append("INBOX", msg("plain"), nil, time.Now())
	acct.Append("INBOX", msg("starred"), []string{store.FlagFlagged}, time.Now())
	acct.Append("INBOX", msg("deal"), []string{store.FlagMarketing}, time.Now())

	list := func() []flaggedMessage {
		req := httptest.NewRequest("GET", "http://127.0.0.1:8080/api/flagged", nil)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		var out []flaggedMessage
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("%d %s", rr.Code, rr.Body)
		}
		return out
	}
	got := list()
	var subjects []string
	for _, m := range got {
		subjects = append(subjects, m.Subject)
	}
	slices.Sort(subjects)
	if !slices.Equal(subjects, []string{"deal", "starred"}) {
		t.Fatalf("flagged: %+v", got)
	}

	for _, m := range got {
		path := "/api/flagged/clear"
		if m.Subject == "deal" {
			path = "/api/flagged/trash"
		}
		body, _ := json.Marshal(flaggedReq{Account: m.Account, Mailbox: m.Mailbox, UID: m.UID})
		if code, out := do(t, h, "POST", path, string(body), hdr); code != 200 {
			t.Fatalf("%s: %d %v", path, code, out)
		}
	}
	if got := list(); len(got) != 0 {
		t.Errorf("after clearing and trashing: %+v", got)
	}
	if st, _ := acct.Status("Trash"); st.Messages != 1 {
		t.Errorf("trash has %d messages", st.Messages)
	}
}
