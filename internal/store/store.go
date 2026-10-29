// Package store persists mailboxes and messages on disk.
//
// Layout:
//
//	<root>/<email>/<mailbox-id>/meta.json   mailbox name, UID state, flags
//	<root>/<email>/<mailbox-id>/<uid>.eml   message (CRLF line endings)
//
// meta.json is the source of truth; message files are written before the
// metadata that references them, so a crash leaves at worst an orphan file.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mailer/internal/users"
)

const (
	FlagSeen     = `\Seen`
	FlagAnswered = `\Answered`
	FlagFlagged  = `\Flagged`
	FlagDeleted  = `\Deleted`
	FlagDraft    = `\Draft`

	// Keywords Mailer sets on delivery: a risky attachment, bulk mail.
	FlagRisky     = "$Risky"
	FlagMarketing = "$Marketing"
)

var (
	ErrNoMailbox = errors.New("no such mailbox")
	ErrExists    = errors.New("mailbox already exists")
	ErrBadName   = errors.New("invalid mailbox name")
)

const Delimiter = "/"

// DefaultMailboxes are created for every account, with RFC 6154 special-use
// attributes so Outlook and Gmail map them to their own folders.
var DefaultMailboxes = []struct{ Name, SpecialUse string }{
	{"INBOX", ""},
	{"Sent", `\Sent`},
	{"Drafts", `\Drafts`},
	{"Trash", `\Trash`},
	{"Junk", `\Junk`},
	{"Archive", `\Archive`},
}

type MessageInfo struct {
	UID   uint32    `json:"uid"`
	Flags []string  `json:"flags,omitempty"`
	Date  time.Time `json:"date"`
	Size  int64     `json:"size"`
}

func (m *MessageInfo) HasFlag(f string) bool {
	for _, x := range m.Flags {
		if strings.EqualFold(x, f) {
			return true
		}
	}
	return false
}

func (m *MessageInfo) clone() *MessageInfo {
	c := *m
	c.Flags = slices.Clone(m.Flags)
	return &c
}

type mailbox struct {
	id          string
	Name        string         `json:"name"`
	SpecialUse  string         `json:"special_use,omitempty"`
	Subscribed  bool           `json:"subscribed"`
	UIDValidity uint32         `json:"uid_validity"`
	UIDNext     uint32         `json:"uid_next"`
	Messages    []*MessageInfo `json:"messages"`
}

func (mb *mailbox) find(uid uint32) (int, bool) {
	return sort.Find(len(mb.Messages), func(i int) int {
		u := mb.Messages[i].UID
		switch {
		case uid < u:
			return -1
		case uid > u:
			return 1
		}
		return 0
	})
}

type Store struct {
	root     string
	mu       sync.Mutex
	accounts map[string]*Account
}

func New(root string) *Store {
	return &Store{root: root, accounts: map[string]*Account{}}
}

// Account opens (creating if needed) the mail account for email.
func (s *Store) Account(email string) (*Account, error) {
	email = strings.ToLower(email)
	if email == "" || strings.ContainsAny(email, `/\`) || strings.HasPrefix(email, ".") {
		return nil, fmt.Errorf("invalid account name %q", email)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.accounts[email]; ok {
		return a, nil
	}
	a := &Account{
		dir:      filepath.Join(s.root, email),
		boxes:    map[string]*mailbox{},
		watchers: map[chan struct{}]struct{}{},
	}
	if err := a.load(); err != nil {
		return nil, err
	}
	s.accounts[email] = a
	return a, nil
}

type Account struct {
	dir      string
	mu       sync.Mutex
	boxes    map[string]*mailbox // keyed by canonical name
	watchers map[chan struct{}]struct{}
}

func canon(name string) string {
	if strings.EqualFold(name, "INBOX") {
		return "INBOX"
	}
	return name
}

func ValidateName(name string) error {
	if name == "" || len(name) > 255 || strings.HasPrefix(name, Delimiter) ||
		strings.HasSuffix(name, Delimiter) || strings.Contains(name, Delimiter+Delimiter) ||
		strings.ContainsAny(name, "*%") {
		return ErrBadName
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return ErrBadName
		}
	}
	return nil
}

var uidValidityMu sync.Mutex
var lastUIDValidity uint32

func newUIDValidity() uint32 {
	uidValidityMu.Lock()
	defer uidValidityMu.Unlock()
	v := uint32(time.Now().Unix())
	if v <= lastUIDValidity {
		v = lastUIDValidity + 1
	}
	lastUIDValidity = v
	return v
}

func (a *Account) load() error {
	if err := os.MkdirAll(a.dir, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(a.dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.dir, e.Name(), "meta.json"))
		if err != nil {
			continue
		}
		mb := &mailbox{id: e.Name()}
		if err := json.Unmarshal(b, mb); err != nil {
			return fmt.Errorf("corrupt mailbox metadata %s: %w", e.Name(), err)
		}
		a.boxes[canon(mb.Name)] = mb
	}
	for _, d := range DefaultMailboxes {
		if _, ok := a.boxes[d.Name]; !ok {
			if err := a.create(d.Name, d.SpecialUse); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Account) create(name, specialUse string) error {
	id := make([]byte, 8)
	rand.Read(id)
	mb := &mailbox{
		id:          hex.EncodeToString(id),
		Name:        name,
		SpecialUse:  specialUse,
		Subscribed:  true,
		UIDValidity: newUIDValidity(),
		UIDNext:     1,
	}
	if err := os.MkdirAll(filepath.Join(a.dir, mb.id), 0o700); err != nil {
		return err
	}
	if err := a.save(mb); err != nil {
		return err
	}
	a.boxes[canon(name)] = mb
	return nil
}

func (a *Account) save(mb *mailbox) error {
	b, err := json.Marshal(mb)
	if err != nil {
		return err
	}
	return users.WriteFileAtomic(filepath.Join(a.dir, mb.id, "meta.json"), b, 0o600)
}

func (a *Account) path(mb *mailbox, uid uint32) string {
	return filepath.Join(a.dir, mb.id, strconv.FormatUint(uint64(uid), 10)+".eml")
}

func (a *Account) get(name string) (*mailbox, error) {
	mb, ok := a.boxes[canon(name)]
	if !ok {
		return nil, ErrNoMailbox
	}
	return mb, nil
}

// Watch returns a channel that receives a value whenever anything in the
// account changes. Call the returned function to stop watching.
func (a *Account) Watch() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	a.mu.Lock()
	a.watchers[ch] = struct{}{}
	a.mu.Unlock()
	return ch, func() {
		a.mu.Lock()
		delete(a.watchers, ch)
		a.mu.Unlock()
	}
}

func (a *Account) notify() {
	for ch := range a.watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

type MailboxInfo struct {
	Name       string
	SpecialUse string
	Subscribed bool
}

func (a *Account) List() []MailboxInfo {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]MailboxInfo, 0, len(a.boxes))
	for _, mb := range a.boxes {
		out = append(out, MailboxInfo{mb.Name, mb.SpecialUse, mb.Subscribed})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name == "INBOX" {
			return true
		}
		if out[j].Name == "INBOX" {
			return false
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// FindSpecialUse returns the name of the mailbox with the given attribute.
func (a *Account) FindSpecialUse(attr string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, mb := range a.boxes {
		if mb.SpecialUse == attr {
			return mb.Name
		}
	}
	return ""
}

func (a *Account) Create(name string) error {
	if err := ValidateName(name); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.boxes[canon(name)]; ok {
		return ErrExists
	}
	if err := a.create(name, ""); err != nil {
		return err
	}
	a.notify()
	return nil
}

func (a *Account) Delete(name string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	mb, err := a.get(name)
	if err != nil {
		return err
	}
	if mb.Name == "INBOX" {
		return errors.New("cannot delete INBOX")
	}
	for n := range a.boxes {
		if strings.HasPrefix(n, mb.Name+Delimiter) {
			return errors.New("mailbox has children; delete them first")
		}
	}
	delete(a.boxes, canon(mb.Name))
	a.notify()
	return os.RemoveAll(filepath.Join(a.dir, mb.id))
}

func (a *Account) Rename(oldName, newName string) error {
	if err := ValidateName(newName); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	mb, err := a.get(oldName)
	if err != nil {
		return err
	}
	if mb.Name == "INBOX" {
		return errors.New("renaming INBOX is not supported")
	}
	if _, ok := a.boxes[canon(newName)]; ok {
		return ErrExists
	}
	// Rename the mailbox and all of its children.
	prefix := mb.Name + Delimiter
	var moved []*mailbox
	for _, b := range a.boxes {
		if b == mb || strings.HasPrefix(b.Name, prefix) {
			moved = append(moved, b)
		}
	}
	for _, b := range moved {
		delete(a.boxes, canon(b.Name))
		b.Name = newName + strings.TrimPrefix(b.Name, mb.Name)
		a.boxes[canon(b.Name)] = b
		if err := a.save(b); err != nil {
			return err
		}
	}
	a.notify()
	return nil
}

func (a *Account) SetSubscribed(name string, sub bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	mb, err := a.get(name)
	if err != nil {
		return err
	}
	mb.Subscribed = sub
	return a.save(mb)
}

type Status struct {
	Messages, Unseen     int
	UIDNext, UIDValidity uint32
	Size                 int64
}

func (a *Account) Status(name string) (Status, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	mb, err := a.get(name)
	if err != nil {
		return Status{}, err
	}
	st := Status{Messages: len(mb.Messages), UIDNext: mb.UIDNext, UIDValidity: mb.UIDValidity}
	for _, m := range mb.Messages {
		if !m.HasFlag(FlagSeen) {
			st.Unseen++
		}
		st.Size += m.Size
	}
	return st, nil
}

// Snapshot is a point-in-time copy of a mailbox's message list.
type Snapshot struct {
	Name        string
	UIDValidity uint32
	UIDNext     uint32
	Messages    []*MessageInfo
}

func (a *Account) Snapshot(name string) (*Snapshot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	mb, err := a.get(name)
	if err != nil {
		return nil, err
	}
	s := &Snapshot{Name: mb.Name, UIDValidity: mb.UIDValidity, UIDNext: mb.UIDNext,
		Messages: make([]*MessageInfo, len(mb.Messages))}
	for i, m := range mb.Messages {
		s.Messages[i] = m.clone()
	}
	return s, nil
}

// Append stores a message and returns its UID and the mailbox UIDVALIDITY.
func (a *Account) Append(name string, raw []byte, flags []string, date time.Time) (uid, uidValidity uint32, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	mb, err := a.get(name)
	if err != nil {
		return 0, 0, err
	}
	if date.IsZero() {
		date = time.Now()
	}
	uid = mb.UIDNext
	if err := users.WriteFileAtomic(a.path(mb, uid), raw, 0o600); err != nil {
		return 0, 0, err
	}
	mb.UIDNext++
	mb.Messages = append(mb.Messages, &MessageInfo{UID: uid, Flags: normalizeFlags(flags), Date: date.Round(time.Second), Size: int64(len(raw))})
	if err := a.save(mb); err != nil {
		return 0, 0, err
	}
	a.notify()
	return uid, mb.UIDValidity, nil
}

// Read returns the raw bytes of a message.
func (a *Account) Read(name string, uid uint32) ([]byte, error) {
	a.mu.Lock()
	mb, err := a.get(name)
	var p string
	if err == nil {
		p = a.path(mb, uid)
	}
	a.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

type FlagOp int

const (
	FlagsReplace FlagOp = iota
	FlagsAdd
	FlagsRemove
)

// SetFlags modifies flags on the given UIDs and returns the updated infos.
func (a *Account) SetFlags(name string, uids []uint32, op FlagOp, flags []string) ([]*MessageInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	mb, err := a.get(name)
	if err != nil {
		return nil, err
	}
	flags = normalizeFlags(flags)
	var out []*MessageInfo
	for _, uid := range uids {
		i, ok := mb.find(uid)
		if !ok {
			continue
		}
		m := mb.Messages[i]
		switch op {
		case FlagsReplace:
			m.Flags = slices.Clone(flags)
		case FlagsAdd:
			m.Flags = normalizeFlags(append(m.Flags, flags...))
		case FlagsRemove:
			m.Flags = slices.DeleteFunc(m.Flags, func(f string) bool {
				return slices.ContainsFunc(flags, func(g string) bool { return strings.EqualFold(f, g) })
			})
		}
		out = append(out, m.clone())
	}
	if err := a.save(mb); err != nil {
		return nil, err
	}
	a.notify()
	return out, nil
}

// Expunge removes messages flagged \Deleted. If uids is non-nil, only those
// UIDs are considered. It returns the removed UIDs in ascending order.
func (a *Account) Expunge(name string, uids []uint32) ([]uint32, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	mb, err := a.get(name)
	if err != nil {
		return nil, err
	}
	var only map[uint32]bool
	if uids != nil {
		only = map[uint32]bool{}
		for _, u := range uids {
			only[u] = true
		}
	}
	return a.remove(mb, func(m *MessageInfo) bool {
		return m.HasFlag(FlagDeleted) && (only == nil || only[m.UID])
	})
}

func (a *Account) remove(mb *mailbox, match func(*MessageInfo) bool) ([]uint32, error) {
	var removed []uint32
	kept := mb.Messages[:0]
	for _, m := range mb.Messages {
		if match(m) {
			removed = append(removed, m.UID)
		} else {
			kept = append(kept, m)
		}
	}
	mb.Messages = kept
	if len(removed) == 0 {
		return nil, nil
	}
	if err := a.save(mb); err != nil {
		return nil, err
	}
	for _, uid := range removed {
		os.Remove(a.path(mb, uid))
	}
	a.notify()
	return removed, nil
}

// Copy copies messages to dst. It returns the source UIDs that existed and
// the new UIDs assigned in dst, plus dst's UIDVALIDITY.
func (a *Account) Copy(src, dst string, uids []uint32) (srcUIDs, dstUIDs []uint32, uidValidity uint32, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.copy(src, dst, uids)
}

func (a *Account) copy(src, dst string, uids []uint32) (srcUIDs, dstUIDs []uint32, uidValidity uint32, err error) {
	from, err := a.get(src)
	if err != nil {
		return nil, nil, 0, err
	}
	to, err := a.get(dst)
	if err != nil {
		return nil, nil, 0, err
	}
	for _, uid := range uids {
		i, ok := from.find(uid)
		if !ok {
			continue
		}
		m := from.Messages[i]
		raw, err := os.ReadFile(a.path(from, uid))
		if err != nil {
			return nil, nil, 0, err
		}
		newUID := to.UIDNext
		if err := users.WriteFileAtomic(a.path(to, newUID), raw, 0o600); err != nil {
			return nil, nil, 0, err
		}
		to.UIDNext++
		c := m.clone()
		c.UID = newUID
		to.Messages = append(to.Messages, c)
		srcUIDs = append(srcUIDs, uid)
		dstUIDs = append(dstUIDs, newUID)
	}
	if err := a.save(to); err != nil {
		return nil, nil, 0, err
	}
	a.notify()
	return srcUIDs, dstUIDs, to.UIDValidity, nil
}

// Move copies messages to dst and removes them from src.
func (a *Account) Move(src, dst string, uids []uint32) (srcUIDs, dstUIDs []uint32, uidValidity uint32, err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	srcUIDs, dstUIDs, uidValidity, err = a.copy(src, dst, uids)
	if err != nil {
		return
	}
	from, _ := a.get(src)
	moved := map[uint32]bool{}
	for _, u := range srcUIDs {
		moved[u] = true
	}
	_, err = a.remove(from, func(m *MessageInfo) bool { return moved[m.UID] })
	return
}

func normalizeFlags(flags []string) []string {
	var out []string
	for _, f := range flags {
		if f == "" || strings.EqualFold(f, `\Recent`) {
			continue
		}
		// System flags have canonical capitalization.
		for _, sys := range []string{FlagSeen, FlagAnswered, FlagFlagged, FlagDeleted, FlagDraft} {
			if strings.EqualFold(f, sys) {
				f = sys
			}
		}
		if !slices.ContainsFunc(out, func(g string) bool { return strings.EqualFold(f, g) }) {
			out = append(out, f)
		}
	}
	return out
}
