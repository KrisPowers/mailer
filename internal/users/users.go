// Package users stores mail accounts and verifies passwords.
package users

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const iterations = 600_000

type record struct {
	Email string `json:"email"`
	Hash  string `json:"hash"` // pbkdf2-sha256$iterations$salt$key
	Locks
}

// Locks are an administrator's per-account switches. Each one stops one
// thing and leaves the others working.
type Locks struct {
	Receive bool `json:"lock_receive,omitempty"` // incoming mail is refused
	Send    bool `json:"lock_send,omitempty"`    // the account can't send
	Login   bool `json:"lock_login,omitempty"`   // no mail app can sign in
}

// Locks returns an account's locks.
func (db *DB) Locks(email string) Locks {
	db.mu.RLock()
	defer db.mu.RUnlock()
	if r, ok := db.users[strings.ToLower(email)]; ok {
		return r.Locks
	}
	return Locks{}
}

func (db *DB) SetLocks(email string, l Locks) error {
	email = strings.ToLower(email)
	db.mu.Lock()
	defer db.mu.Unlock()
	r, ok := db.users[email]
	if !ok {
		return fmt.Errorf("no such user %s", email)
	}
	r.Locks = l
	return db.save()
}

// DB is a JSON-file-backed user database.
type DB struct {
	path  string
	mu    sync.RWMutex
	users map[string]*record
}

func Open(path string) (*DB, error) {
	db := &DB{path: path, users: map[string]*record{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return db, nil
	}
	if err != nil {
		return nil, err
	}
	var list []*record
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, r := range list {
		db.users[r.Email] = r
	}
	return db, nil
}

// Normalize lowercases and validates an address.
func Normalize(email string) (string, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || !strings.Contains(email, "@") {
		return "", fmt.Errorf("invalid address %q", email)
	}
	return email, nil
}

func (db *DB) Add(email, password string) error {
	email, err := Normalize(email)
	if err != nil {
		return err
	}
	if strings.Contains(strings.SplitN(email, "@", 2)[0], "+") {
		return fmt.Errorf("local part may not contain '+' (reserved for plus-addressing)")
	}
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, ok := db.users[email]; ok {
		return fmt.Errorf("user %s already exists", email)
	}
	h, err := hash(password)
	if err != nil {
		return err
	}
	db.users[email] = &record{Email: email, Hash: h}
	return db.save()
}

func (db *DB) SetPassword(email, password string) error {
	email = strings.ToLower(email)
	db.mu.Lock()
	defer db.mu.Unlock()
	r, ok := db.users[email]
	if !ok {
		return fmt.Errorf("no such user %s", email)
	}
	h, err := hash(password)
	if err != nil {
		return err
	}
	r.Hash = h
	return db.save()
}

func (db *DB) Delete(email string) error {
	email = strings.ToLower(email)
	db.mu.Lock()
	defer db.mu.Unlock()
	if _, ok := db.users[email]; !ok {
		return fmt.Errorf("no such user %s", email)
	}
	delete(db.users, email)
	return db.save()
}

func (db *DB) Exists(email string) bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	_, ok := db.users[strings.ToLower(email)]
	return ok
}

func (db *DB) List() []string {
	db.mu.RLock()
	defer db.mu.RUnlock()
	var out []string
	for e := range db.users {
		out = append(out, e)
	}
	sort.Strings(out)
	return out
}

// dummyHash is verified against when the user doesn't exist so that timing
// doesn't reveal which accounts exist.
var dummyHash, _ = hash("dummy-password-for-timing")

// Authenticate returns the canonical address on success. An account whose
// sign-in is locked never authenticates; LoginLocked tells callers why.
func (db *DB) Authenticate(email, password string) (string, bool) {
	email = strings.ToLower(strings.TrimSpace(email))
	db.mu.RLock()
	r, ok := db.users[email]
	h := dummyHash
	locked := false
	if ok {
		h, locked = r.Hash, r.Login
	}
	db.mu.RUnlock()
	match := verify(h, password)
	return email, ok && match && !locked
}

// LoginLocked reports whether email exists with its sign-in locked.
func (db *DB) LoginLocked(email string) bool {
	return db.Locks(strings.TrimSpace(email)).Login
}

func (db *DB) save() error {
	list := make([]*record, 0, len(db.users))
	for _, r := range db.users {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Email < list[j].Email })
	b, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(db.path, b, 0o600)
}

func hash(password string) (string, error) {
	if len(password) < 10 {
		return "", errors.New("password must be at least 10 characters")
	}
	salt := make([]byte, 16)
	rand.Read(salt)
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, 32)
	if err != nil {
		return "", err
	}
	enc := base64.RawStdEncoding
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", iterations, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

func verify(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err1 := enc.DecodeString(parts[2])
	want, err2 := enc.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// WriteFileAtomic writes via a temp file and rename so readers never see a
// partially written file.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Limiter blocks an IP after repeated authentication failures.
type Limiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

const (
	maxFailures   = 5
	failureWindow = 15 * time.Minute
)

func NewLimiter() *Limiter { return &Limiter{failures: map[string][]time.Time{}} }

// Blocked reports whether ip has too many recent failures.
func (l *Limiter) Blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(ip)) >= maxFailures
}

// Fail records a failed attempt from ip and reports whether ip is now
// blocked because of it.
func (l *Limiter) Fail(ip string) (nowBlocked bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[ip] = append(l.prune(ip), time.Now())
	return len(l.failures[ip]) == maxFailures
}

// BlockedIPs returns each blocked address with when it's let in again.
func (l *Limiter) BlockedIPs() map[string]time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]time.Time{}
	for ip := range l.failures {
		if ts := l.prune(ip); len(ts) >= maxFailures {
			out[ip] = ts[len(ts)-maxFailures].Add(failureWindow)
		}
	}
	return out
}

// Unblock forgets ip's failures.
func (l *Limiter) Unblock(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, ip)
}

func (l *Limiter) prune(ip string) []time.Time {
	cutoff := time.Now().Add(-failureWindow)
	ts := l.failures[ip]
	i := 0
	for i < len(ts) && ts[i].Before(cutoff) {
		i++
	}
	ts = ts[i:]
	if len(ts) == 0 {
		delete(l.failures, ip)
		return nil
	}
	l.failures[ip] = ts
	return ts
}
