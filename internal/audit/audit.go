// Package audit records every sign-in attempt by a mail app, and every
// connection that left without trying, so the dashboard can show why
// Outlook or the Gmail app can't sign in. The record is kept in
// <data>/signins.log, one JSON event per line.
package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"slices"
	"sync"
	"time"
)

// Results of a sign-in attempt.
const (
	OK          = "ok"
	BadPassword = "bad_password"
	UnknownUser = "unknown_user"
	Blocked     = "blocked"     // too many recent failures from the address
	Locked      = "locked"      // an administrator locked the account's sign-in
	NeedsTLS    = "needs_tls"   // tried to sign in over an unencrypted connection
	Unsupported = "unsupported" // asked for a sign-in method we don't offer
	Malformed   = "malformed"
	NoSignIn    = "no_sign_in" // connected, sent commands, left without signing in
)

// Event is one sign-in attempt or abandoned connection.
type Event struct {
	At     time.Time `json:"at"`
	Proto  string    `json:"proto"` // imap or smtp
	IP     string    `json:"ip"`
	Client string    `json:"client,omitempty"` // the app's IMAP ID, or its SMTP EHLO name
	User   string    `json:"user,omitempty"`
	Result string    `json:"result"`
	Detail string    `json:"detail,omitempty"`
	// Commands the app sent before this, without arguments other than a
	// sign-in method, so it shows how far the app got.
	Commands []string `json:"commands,omitempty"`
}

const (
	keep    = 500     // events held for the dashboard
	maxFile = 5 << 20 // the file is started over past this size
)

// Log holds recent events and appends each one to its file. A nil Log
// records nothing.
type Log struct {
	path   string
	mu     sync.Mutex
	events []Event // oldest first
}

// Open loads the most recent events from path.
func Open(path string) *Log {
	l := &Log{path: path}
	if st, err := os.Stat(path); err == nil && st.Size() > maxFile {
		os.Rename(path, path+".old")
	}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			var e Event
			if json.Unmarshal(sc.Bytes(), &e) == nil {
				l.events = append(l.events, e)
			}
		}
		f.Close()
		l.events = l.events[max(0, len(l.events)-keep):]
	}
	return l
}

func (l *Log) Add(e Event) {
	if l == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now()
	}
	e.Commands = slices.Clone(e.Commands)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, e)
	if len(l.events) > keep {
		l.events = slices.Clone(l.events[len(l.events)-keep:])
	}
	if f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		b, _ := json.Marshal(e)
		f.Write(append(b, '\n'))
		f.Close()
	}
}

// Recent returns the held events, newest first.
func (l *Log) Recent() []Event {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	out := slices.Clone(l.events)
	slices.Reverse(out)
	return out
}
