// Package auth guards the dashboard. Nobody signs up: the first admin is
// made by the setup on the host, and admins make every other account.
// Each browser gets a random device ID on its first visit, kept in its
// localStorage, and can't reach the sign-in until an admin approves it.
// Signing in gives a short-lived token (an HS256 JWT) bound to the session
// and the device, and a refresh token that's replaced on every use.
//
// Everything lives in <data>/auth.json. Deleting that file starts the
// setup again, from the host only; mail and settings are kept.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"mailer/internal/users"
)

const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	// AccessTTL is how long a token lasts; the dashboard refreshes it.
	AccessTTL = 15 * time.Minute
	// A session ends after a week unused, and after 30 days regardless.
	idleTTL       = 7 * 24 * time.Hour
	maxSessionAge = 30 * 24 * time.Hour
	// A replaced refresh token is let through this soon after, without
	// a new one, for tabs that refreshed at the same moment.
	refreshGrace = 20 * time.Second
	// Devices nobody approved are forgotten after a week, and only the
	// newest maxPending are kept, so visitors can't fill the file.
	pendingTTL = 7 * 24 * time.Hour
	maxPending = 50

	MinPassword = 12
)

var (
	ErrCredentials = errors.New("wrong username or password")
	ErrDevice      = errors.New("this device isn't approved for the dashboard")
	ErrSession     = errors.New("signed out, sign in again")
	ErrSetupDone   = errors.New("setup is already done")
)

type User struct {
	Name      string    `json:"name"`
	Hash      string    `json:"hash"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled,omitempty"`
	Created   time.Time `json:"created"`
	CreatedBy string    `json:"created_by,omitempty"`
	LastLogin time.Time `json:"last_login,omitempty"`
}

// UserInfo is a User without its password hash.
type UserInfo struct {
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	Disabled  bool      `json:"disabled"`
	Created   time.Time `json:"created"`
	CreatedBy string    `json:"created_by,omitempty"`
	LastLogin time.Time `json:"last_login,omitempty"`
}

func (u User) info() UserInfo {
	return UserInfo{u.Name, u.Role, u.Disabled, u.Created, u.CreatedBy, u.LastLogin}
}

// Device is a browser that has visited the dashboard. Only a hash of its
// ID is kept, so this file can't be used to pose as it.
type Device struct {
	Key        string    `json:"key"`  // SHA-256 of the device ID
	Code       string    `json:"code"` // the ID's first characters, shown on the device and here to match them
	Approved   bool      `json:"approved"`
	Name       string    `json:"name,omitempty"`
	Agent      string    `json:"agent"`
	IP         string    `json:"ip"`
	FirstSeen  time.Time `json:"first_seen"`
	LastSeen   time.Time `json:"last_seen"`
	ApprovedBy string    `json:"approved_by,omitempty"`
	ApprovedAt time.Time `json:"approved_at,omitempty"`
}

type Session struct {
	ID     string `json:"id"`
	User   string `json:"user"`
	Device string `json:"device"` // Device.Key
	// Hashes of the current refresh token and the one it replaced. The old
	// one coming back means it was copied, so the session is ended.
	Refresh  string    `json:"refresh"`
	Previous string    `json:"previous,omitempty"`
	Rotated  time.Time `json:"rotated,omitempty"`
	IP       string    `json:"ip"`
	Agent    string    `json:"agent"`
	Created  time.Time `json:"created"`
	LastUsed time.Time `json:"last_used"`
}

// SessionInfo is a Session without its refresh token hashes.
type SessionInfo struct {
	ID         string    `json:"id"`
	User       string    `json:"user"`
	Device     string    `json:"device"`
	DeviceCode string    `json:"device_code"`
	DeviceName string    `json:"device_name,omitempty"`
	IP         string    `json:"ip"`
	Agent      string    `json:"agent"`
	Created    time.Time `json:"created"`
	LastUsed   time.Time `json:"last_used"`
}

// Tokens are what signing in or refreshing hands the browser.
type Tokens struct {
	Access  string    `json:"token"`
	Expires time.Time `json:"expires"`
	Refresh string    `json:"-"` // set as an HttpOnly cookie, never readable by the page; empty keeps the cookie
	User    UserInfo  `json:"user"`
}

type state struct {
	Key      []byte    `json:"key"` // signs tokens
	Users    []User    `json:"users"`
	Devices  []Device  `json:"devices"`
	Sessions []Session `json:"sessions"`
}

type Store struct {
	path string
	mu   sync.Mutex
	st   state
	now  func() time.Time
}

func Open(path string) (*Store, error) {
	s := &Store{path: path, now: time.Now}
	b, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(b, &s.st); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
	}
	if len(s.st.Key) < 32 {
		s.st.Key = random(32)
	}
	return s, s.save()
}

// NeedsSetup reports whether there's no account yet.
func (s *Store) NeedsSetup() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.st.Users) == 0
}

// NewDevice gives a browser its ID, waiting for approval.
func (s *Store) NewDevice(agent, ip string) (string, Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	id := hex.EncodeToString(random(24))
	d := Device{Key: digest(id), Code: codeOf(id), Agent: clip(agent, 200), IP: ip, FirstSeen: now, LastSeen: now}
	s.prune(now)
	var pending []int
	for i, x := range s.st.Devices {
		if !x.Approved {
			pending = append(pending, i)
		}
	}
	if len(pending) >= maxPending {
		// The oldest go first; Devices is in the order they arrived.
		drop := map[int]bool{}
		for _, i := range pending[:len(pending)-maxPending+1] {
			drop[i] = true
		}
		kept := s.st.Devices[:0]
		for i, x := range s.st.Devices {
			if !drop[i] {
				kept = append(kept, x)
			}
		}
		s.st.Devices = kept
	}
	s.st.Devices = append(s.st.Devices, d)
	return id, d, s.save()
}

// Device looks up a device by the ID the browser holds.
func (s *Store) Device(id string) (Device, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.device(digest(id))
	if d == nil || id == "" {
		return Device{}, false
	}
	return *d, true
}

// Setup makes the first admin and approves the device doing it. It works
// once, while there's no account.
func (s *Store) Setup(name, password, deviceID, ip, agent string) (Tokens, error) {
	name, err := checkName(name)
	if err != nil {
		return Tokens{}, err
	}
	hash, err := hashPassword(password)
	if err != nil {
		return Tokens{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.st.Users) > 0 {
		return Tokens{}, ErrSetupDone
	}
	now := s.now()
	d := s.device(digest(deviceID))
	if d == nil || deviceID == "" {
		return Tokens{}, errors.New("this browser has no device ID yet; reload the page")
	}
	d.Approved, d.ApprovedBy, d.ApprovedAt, d.Name = true, name, now, "Setup device"
	s.st.Users = append(s.st.Users, User{Name: name, Hash: hash, Role: RoleAdmin, Created: now})
	return s.startSession(name, d, ip, agent)
}

// dummyHash is checked against for unknown names, so a missing account
// takes as long to refuse as a wrong password.
var dummyHash, _ = hashPassword("not-a-real-password-at-all")

// Login signs name in on an approved device.
func (s *Store) Login(name, password, deviceID, ip, agent string) (Tokens, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	s.mu.Lock()
	d := s.device(digest(deviceID))
	if d == nil || !d.Approved || deviceID == "" {
		s.mu.Unlock()
		return Tokens{}, ErrDevice
	}
	hash, active := dummyHash, false
	if u := s.user(name); u != nil {
		hash, active = u.Hash, !u.Disabled
	}
	s.mu.Unlock()

	// The hash is slow on purpose; it runs without holding the lock.
	if !users.VerifyPassword(hash, password) || !active {
		return Tokens{}, ErrCredentials
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, d := s.user(name), s.device(digest(deviceID))
	if u == nil || u.Disabled || d == nil || !d.Approved {
		return Tokens{}, ErrCredentials
	}
	u.LastLogin = s.now()
	return s.startSession(name, d, ip, agent)
}

// startSession is called with the lock held.
func (s *Store) startSession(name string, d *Device, ip, agent string) (Tokens, error) {
	now := s.now()
	d.LastSeen, d.IP = now, ip
	sess := Session{ID: hex.EncodeToString(random(16)), User: name, Device: d.Key, IP: ip, Agent: clip(agent, 200),
		Created: now, LastUsed: now}
	refresh := sess.ID + "." + hex.EncodeToString(random(32))
	sess.Refresh = digest(refresh)
	s.prune(now)
	s.st.Sessions = append(s.st.Sessions, sess)
	if err := s.save(); err != nil {
		return Tokens{}, err
	}
	return s.issue(sess, refresh), nil
}

// issue is called with the lock held.
func (s *Store) issue(sess Session, refresh string) Tokens {
	now := s.now()
	u := s.user(sess.User)
	exp := now.Add(AccessTTL)
	access := sign(s.st.Key, Claims{Issuer: issuer, Subject: u.Name, Role: u.Role, Session: sess.ID, Device: sess.Device,
		Issued: now.Unix(), Expires: exp.Unix()})
	return Tokens{Access: access, Expires: exp, Refresh: refresh, User: u.info()}
}

// Refresh swaps a refresh token for a new pair. A token that was already
// swapped ends the session: whoever holds it copied it.
func (s *Store) Refresh(refresh, deviceID, ip string) (Tokens, error) {
	sid, _, _ := strings.Cut(refresh, ".")
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.prune(now)
	i := s.session(sid)
	if i < 0 || sid == "" {
		return Tokens{}, ErrSession
	}
	sess := &s.st.Sessions[i]
	h := digest(refresh)
	d, u := s.device(sess.Device), s.user(sess.User)
	valid := sess.Device == digest(deviceID) && d != nil && d.Approved && u != nil && !u.Disabled
	replayed := sess.Previous != "" && same(h, sess.Previous)
	switch {
	case valid && same(h, sess.Refresh):
	case valid && replayed && now.Sub(sess.Rotated) < refreshGrace:
		// Two tabs of one browser refreshed at once with the same cookie;
		// the other tab already set the new one.
		return s.issue(*sess, ""), nil
	case replayed:
		s.st.Sessions = slices.Delete(s.st.Sessions, i, i+1)
		s.save()
		return Tokens{}, ErrSession
	default:
		return Tokens{}, ErrSession
	}
	next := sess.ID + "." + hex.EncodeToString(random(32))
	sess.Previous, sess.Refresh, sess.Rotated = sess.Refresh, digest(next), now
	sess.LastUsed, sess.IP = now, ip
	d.LastSeen, d.IP = now, ip
	if err := s.save(); err != nil {
		return Tokens{}, err
	}
	return s.issue(*sess, next), nil
}

// Logout ends the session a refresh token belongs to.
func (s *Store) Logout(refresh string) {
	sid, _, _ := strings.Cut(refresh, ".")
	s.mu.Lock()
	defer s.mu.Unlock()
	if i := s.session(sid); i >= 0 && sid != "" {
		h := digest(refresh)
		if same(h, s.st.Sessions[i].Refresh) || same(h, s.st.Sessions[i].Previous) {
			s.st.Sessions = slices.Delete(s.st.Sessions, i, i+1)
			s.save()
		}
	}
}

// Verify checks a token from deviceID and returns its claims, with the
// user's current role. The session, account and device are checked on
// every call, so ending any of them takes effect at once.
func (s *Store) Verify(token, deviceID string) (Claims, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	c, err := parse(s.st.Key, token, now)
	if err != nil {
		return c, err
	}
	i := s.session(c.Session)
	if i < 0 || c.Device != digest(deviceID) || s.st.Sessions[i].Device != c.Device || s.st.Sessions[i].User != c.Subject ||
		now.Sub(s.st.Sessions[i].LastUsed) > idleTTL || now.Sub(s.st.Sessions[i].Created) > maxSessionAge {
		return c, ErrSession
	}
	d, u := s.device(c.Device), s.user(c.Subject)
	if d == nil || !d.Approved {
		return c, ErrDevice
	}
	if u == nil || u.Disabled {
		return c, ErrSession
	}
	c.Role = u.Role
	return c, nil
}

// ---- accounts ----

func (s *Store) Users() []UserInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []UserInfo{}
	for _, u := range s.st.Users {
		out = append(out, u.info())
	}
	return out
}

func (s *Store) User(name string) (UserInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if u := s.user(name); u != nil {
		return u.info(), true
	}
	return UserInfo{}, false
}

func (s *Store) AddUser(name, password, role, by string) (UserInfo, error) {
	name, err := checkName(name)
	if err != nil {
		return UserInfo{}, err
	}
	if role != RoleAdmin && role != RoleUser {
		return UserInfo{}, fmt.Errorf("unknown role %q", role)
	}
	hash, err := hashPassword(password)
	if err != nil {
		return UserInfo{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.user(name) != nil {
		return UserInfo{}, fmt.Errorf("%s already exists", name)
	}
	u := User{Name: name, Hash: hash, Role: role, Created: s.now(), CreatedBy: by}
	s.st.Users = append(s.st.Users, u)
	return u.info(), s.save()
}

// SetPassword changes name's password and ends its sessions, except keep
// (the session changing its own password).
func (s *Store) SetPassword(name, password, keep string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.user(name)
	if u == nil {
		return fmt.Errorf("no account %q", name)
	}
	u.Hash = hash
	s.endSessions(func(x Session) bool { return x.User == name && x.ID != keep })
	return s.save()
}

// CheckPassword reports whether password is name's.
func (s *Store) CheckPassword(name, password string) bool {
	s.mu.Lock()
	hash := dummyHash
	if u := s.user(name); u != nil {
		hash = u.Hash
	}
	s.mu.Unlock()
	return users.VerifyPassword(hash, password)
}

// SetRole changes name's role. The last active admin stays an admin.
func (s *Store) SetRole(name, role string) error {
	if role != RoleAdmin && role != RoleUser {
		return fmt.Errorf("unknown role %q", role)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.user(name)
	if u == nil {
		return fmt.Errorf("no account %q", name)
	}
	if role != RoleAdmin && s.lastAdmin(name) {
		return errors.New("there has to be at least one admin")
	}
	u.Role = role
	return s.save()
}

// SetDisabled turns name's sign-in off or on; off ends its sessions.
func (s *Store) SetDisabled(name string, disabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.user(name)
	if u == nil {
		return fmt.Errorf("no account %q", name)
	}
	if disabled && s.lastAdmin(name) {
		return errors.New("the last admin can't be turned off")
	}
	u.Disabled = disabled
	if disabled {
		s.endSessions(func(x Session) bool { return x.User == name })
	}
	return s.save()
}

func (s *Store) DeleteUser(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.user(name) == nil {
		return fmt.Errorf("no account %q", name)
	}
	if s.lastAdmin(name) {
		return errors.New("the last admin can't be deleted")
	}
	s.st.Users = slices.DeleteFunc(s.st.Users, func(u User) bool { return u.Name == name })
	s.endSessions(func(x Session) bool { return x.User == name })
	return s.save()
}

// lastAdmin reports whether name is the only active admin.
func (s *Store) lastAdmin(name string) bool {
	n, is := 0, false
	for _, u := range s.st.Users {
		if u.Role == RoleAdmin && !u.Disabled {
			n++
			is = is || u.Name == name
		}
	}
	return is && n == 1
}

// ---- devices ----

// Devices lists every device, waiting ones first, then the most recent.
func (s *Store) Devices() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(s.now())
	out := slices.Clone(s.st.Devices)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Approved != out[j].Approved {
			return !out[i].Approved
		}
		return out[i].LastSeen.After(out[j].LastSeen)
	})
	if out == nil {
		out = []Device{}
	}
	return out
}

// Approve lets a device reach the sign-in.
func (s *Store) Approve(key, name, by string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.device(key)
	if d == nil {
		return errors.New("no such device")
	}
	d.Approved, d.ApprovedBy, d.ApprovedAt, d.Name = true, by, s.now(), clip(strings.TrimSpace(name), 60)
	return s.save()
}

func (s *Store) RenameDevice(key, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.device(key)
	if d == nil {
		return errors.New("no such device")
	}
	d.Name = clip(strings.TrimSpace(name), 60)
	return s.save()
}

// RemoveDevice forgets a device and ends its sessions. It gets a new ID,
// waiting for approval, the next time it visits.
func (s *Store) RemoveDevice(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.device(key) == nil {
		return errors.New("no such device")
	}
	s.st.Devices = slices.DeleteFunc(s.st.Devices, func(d Device) bool { return d.Key == key })
	s.endSessions(func(x Session) bool { return x.Device == key })
	return s.save()
}

// ---- sessions ----

// Sessions lists name's sessions, or everyone's when name is empty.
func (s *Store) Sessions(name string) []SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(s.now())
	out := []SessionInfo{}
	for _, x := range s.st.Sessions {
		if name != "" && x.User != name {
			continue
		}
		si := SessionInfo{ID: x.ID, User: x.User, Device: x.Device, IP: x.IP, Agent: x.Agent, Created: x.Created, LastUsed: x.LastUsed}
		if d := s.device(x.Device); d != nil {
			si.DeviceCode, si.DeviceName = d.Code, d.Name
		}
		out = append(out, si)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastUsed.After(out[j].LastUsed) })
	return out
}

// EndSession ends one session; with owner set, only if it's theirs.
func (s *Store) EndSession(id, owner string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.session(id)
	if i < 0 || owner != "" && s.st.Sessions[i].User != owner {
		return errors.New("no such session")
	}
	s.st.Sessions = slices.Delete(s.st.Sessions, i, i+1)
	return s.save()
}

// ---- helpers, called with the lock held ----

func (s *Store) user(name string) *User {
	for i := range s.st.Users {
		if s.st.Users[i].Name == name {
			return &s.st.Users[i]
		}
	}
	return nil
}

func (s *Store) device(key string) *Device {
	for i := range s.st.Devices {
		if same(s.st.Devices[i].Key, key) {
			return &s.st.Devices[i]
		}
	}
	return nil
}

func (s *Store) session(id string) int {
	for i := range s.st.Sessions {
		if s.st.Sessions[i].ID == id {
			return i
		}
	}
	return -1
}

func (s *Store) endSessions(match func(Session) bool) {
	s.st.Sessions = slices.DeleteFunc(s.st.Sessions, match)
}

// prune drops expired sessions and stale unapproved devices.
func (s *Store) prune(now time.Time) {
	s.endSessions(func(x Session) bool {
		return now.Sub(x.LastUsed) > idleTTL || now.Sub(x.Created) > maxSessionAge
	})
	s.st.Devices = slices.DeleteFunc(s.st.Devices, func(d Device) bool {
		return !d.Approved && now.Sub(d.LastSeen) > pendingTTL
	})
}

func (s *Store) save() error {
	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	return users.WriteFileAtomic(s.path, b, 0o600)
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,31}$`)

func checkName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !nameRe.MatchString(name) {
		return "", errors.New("usernames are 3 to 32 letters, digits, dots, dashes or underscores")
	}
	return name, nil
}

func hashPassword(password string) (string, error) {
	if len(password) < MinPassword {
		return "", fmt.Errorf("password must be at least %d characters", MinPassword)
	}
	if len(password) > 256 {
		return "", errors.New("password is too long")
	}
	return users.HashPassword(password)
}

func random(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func digest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func same(a, b string) bool { return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1 }

// codeOf is the part of a device ID shown to match it, like 3F9A-C21B.
func codeOf(id string) string {
	return strings.ToUpper(id[:4] + "-" + id[4:8])
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
