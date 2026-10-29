package admin

import (
	"errors"
	"net/http"
	"slices"
	"sort"
	"time"

	"mailer/internal/message"
	"mailer/internal/store"
)

// flaggedMessage is a message someone flagged in their mail app, or that
// Mailer flagged on delivery for a risky attachment or as bulk mail.
type flaggedMessage struct {
	Account string    `json:"account"`
	Mailbox string    `json:"mailbox"`
	UID     uint32    `json:"uid"`
	Date    time.Time `json:"date"`
	Size    int64     `json:"size"`
	From    string    `json:"from"`
	Subject string    `json:"subject"`
	Reasons []string  `json:"reasons"` // flagged, risky, marketing
	Files   []string  `json:"files,omitempty"`
}

var flagReasons = []struct{ flag, reason string }{
	{store.FlagFlagged, "flagged"},
	{store.FlagRisky, "risky"},
	{store.FlagMarketing, "marketing"},
}

const maxFlagged = 500

// eachFlagged calls fn for every flagged message in every account, leaving
// out Trash and messages marked deleted.
func (s *Server) eachFlagged(fn func(email string, acct *store.Account, mailbox string, m *store.MessageInfo, reasons []string)) {
	for _, email := range s.Users.List() {
		acct, err := s.Store.Account(email)
		if err != nil {
			continue
		}
		for _, mb := range acct.List() {
			if mb.SpecialUse == `\Trash` {
				continue
			}
			snap, err := acct.Snapshot(mb.Name)
			if err != nil {
				continue
			}
			for _, m := range snap.Messages {
				var reasons []string
				for _, f := range flagReasons {
					if m.HasFlag(f.flag) {
						reasons = append(reasons, f.reason)
					}
				}
				if len(reasons) > 0 && !m.HasFlag(store.FlagDeleted) {
					fn(email, acct, mb.Name, m, reasons)
				}
			}
		}
	}
}

// flaggedCount is the number on the sidebar; it reads no messages.
func (s *Server) flaggedCount() int {
	n := 0
	s.eachFlagged(func(string, *store.Account, string, *store.MessageInfo, []string) { n++ })
	return n
}

// getFlagged lists the flagged messages, newest first.
func (s *Server) getFlagged(w http.ResponseWriter, r *http.Request) {
	out := []flaggedMessage{}
	s.eachFlagged(func(email string, acct *store.Account, mailbox string, m *store.MessageInfo, reasons []string) {
		fm := flaggedMessage{Account: email, Mailbox: mailbox, UID: m.UID, Date: m.Date, Size: m.Size, Reasons: reasons}
		if raw, err := acct.Read(mailbox, m.UID); err == nil {
			hdr, _ := message.SplitHeader(raw)
			h := message.ParseHeader(hdr)
			fm.From = message.DecodeHeader(h.Get("From"))
			fm.Subject = message.DecodeHeader(h.Get("Subject"))
			if slices.Contains(reasons, "risky") {
				fm.Files = message.RiskyFiles(raw)
			}
		}
		out = append(out, fm)
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Date.After(out[j].Date) })
	if len(out) > maxFlagged {
		out = out[:maxFlagged]
	}
	writeJSON(w, out)
}

type flaggedReq struct {
	Account string `json:"account"`
	Mailbox string `json:"mailbox"`
	UID     uint32 `json:"uid"`
}

// flaggedAccount reads a request about one flagged message and opens its
// account.
func (s *Server) flaggedAccount(w http.ResponseWriter, r *http.Request) (*store.Account, flaggedReq, bool) {
	var req flaggedReq
	if err := readJSON(r, &req); err != nil {
		writeErr(w, 400, err)
		return nil, req, false
	}
	if !s.Users.Exists(req.Account) {
		writeErr(w, 404, errors.New("no such account"))
		return nil, req, false
	}
	acct, err := s.Store.Account(req.Account)
	if err != nil {
		writeErr(w, 500, err)
		return nil, req, false
	}
	return acct, req, true
}

// clearFlags removes every flag the Flagged page lists from one message.
func (s *Server) clearFlags(w http.ResponseWriter, r *http.Request) {
	acct, req, ok := s.flaggedAccount(w, r)
	if !ok {
		return
	}
	if _, err := acct.SetFlags(req.Mailbox, []uint32{req.UID}, store.FlagsRemove,
		[]string{store.FlagFlagged, store.FlagRisky, store.FlagMarketing}); err != nil {
		writeErr(w, 400, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// trashFlagged moves one flagged message to its account's Trash.
func (s *Server) trashFlagged(w http.ResponseWriter, r *http.Request) {
	acct, req, ok := s.flaggedAccount(w, r)
	if !ok {
		return
	}
	trash := acct.FindSpecialUse(`\Trash`)
	if trash == "" {
		writeErr(w, 400, errors.New("this account has no Trash folder"))
		return
	}
	if _, _, _, err := acct.Move(req.Mailbox, trash, []uint32{req.UID}); err != nil {
		writeErr(w, 400, err)
		return
	}
	s.Log.Printf("admin: %s moved a flagged message in %s to Trash", claimsOf(r).Subject, req.Account)
	writeJSON(w, map[string]bool{"ok": true})
}
