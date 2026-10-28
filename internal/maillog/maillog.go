// Package maillog records what happened to each message, received or sent,
// for the dashboard's Queue. It keeps the newest entries in memory and in
// <data>/mail.log, one JSON entry per line.
package maillog

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"sync"
	"time"
)

// Directions and outcomes.
const (
	In  = "in"
	Out = "out"

	Delivered = "delivered" // stored in a local mailbox
	Junk      = "junk"      // stored, in Junk
	Rejected  = "rejected"  // refused for good
	Failed    = "failed"    // refused for now; the sender tries again
	Sent      = "sent"      // taken by Cloudflare Email Sending
	Bounced   = "bounced"   // gave up; the sender was told
)

type Entry struct {
	At     time.Time `json:"at"`
	Dir    string    `json:"dir"`
	From   string    `json:"from"`
	To     string    `json:"to"`
	Size   int       `json:"size"`
	Status string    `json:"status"`
	Detail string    `json:"detail,omitempty"`
}

const (
	keep    = 1000
	maxFile = 5 << 20
)

// Log holds recent entries and appends each one to its file. A nil Log
// records nothing.
type Log struct {
	path    string
	mu      sync.Mutex
	entries []Entry // oldest first
}

// Open loads the most recent entries from path.
func Open(path string) *Log {
	l := &Log{path: path}
	if st, err := os.Stat(path); err == nil && st.Size() > maxFile {
		os.Rename(path, path+".old")
	}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var e Entry
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				l.entries = append(l.entries, e)
			}
		}
		f.Close()
		l.entries = l.entries[max(0, len(l.entries)-keep):]
	}
	return l
}

func (l *Log) Add(e Entry) {
	if l == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, e)
	if len(l.entries) > keep {
		l.entries = slices.Clone(l.entries[len(l.entries)-keep:])
	}
	if f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		b, _ := json.Marshal(e)
		f.Write(append(b, '\n'))
		f.Close()
	}
}

// Recent returns the held entries, newest first.
func (l *Log) Recent() []Entry {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := slices.Clone(l.entries)
	slices.Reverse(out)
	return out
}
