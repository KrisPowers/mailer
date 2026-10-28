package maillog

import (
	"path/filepath"
	"testing"
)

func TestLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.log")
	l := Open(path)
	for range keep + 5 {
		l.Add(Entry{Dir: In, From: "a@other.net", To: "me@example.com", Status: Delivered})
	}
	l.Add(Entry{Dir: Out, From: "me@example.com", To: "b@other.net", Status: Bounced, Detail: "no such user"})

	// A restart reads the same entries back, newest first.
	for _, l := range []*Log{l, Open(path)} {
		got := l.Recent()
		if len(got) != keep || got[0].Status != Bounced || got[0].Detail != "no such user" || got[1].Dir != In {
			t.Fatalf("recent: %d entries, first %+v", len(got), got[0])
		}
	}

	var none *Log
	none.Add(Entry{Status: Sent})
	if none.Recent() != nil {
		t.Error("nil log recorded")
	}
}
