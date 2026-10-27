package audit

import (
	"path/filepath"
	"testing"
)

func TestLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signins.log")
	l := Open(path)
	for i := range keep + 10 {
		l.Add(Event{Proto: "imap", IP: "203.0.113.9", User: "me@example.com", Result: BadPassword, Detail: string(rune('a' + i%26))})
	}
	l.Add(Event{Proto: "smtp", Result: OK, Commands: []string{"EHLO", "AUTH PLAIN"}})

	// A restart reads the same recent events back, newest first.
	for _, l := range []*Log{l, Open(path)} {
		got := l.Recent()
		if len(got) != keep || got[0].Result != OK || got[0].Commands[1] != "AUTH PLAIN" || got[1].Result != BadPassword {
			t.Fatalf("recent: %d events, first %+v", len(got), got[0])
		}
	}

	var none *Log
	none.Add(Event{Result: OK})
	if none.Recent() != nil {
		t.Error("nil log recorded")
	}
}
