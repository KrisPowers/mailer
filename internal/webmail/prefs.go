package webmail

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"mailer/internal/store"
	"mailer/internal/users"
)

// Prefs are an account's webmail settings: its name on what it sends, its
// signatures, its categories and its rules. They follow the account to
// any browser.
type Prefs struct {
	Name       string      `json:"name"`
	Signatures []Signature `json:"signatures"`
	// The signature put under a new message, and under a reply or a
	// forward; "" for none.
	NewSignature   string     `json:"new_signature"`
	ReplySignature string     `json:"reply_signature"`
	Categories     []Category `json:"categories"`
	Rules          []Rule     `json:"rules"`
	// KeepUnread leaves a message unread when it's opened.
	KeepUnread bool `json:"keep_unread"`
	// Images loads pictures from the internet in every message, not only
	// after asking.
	Images bool `json:"images"`
}

type Signature struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	HTML string `json:"html"`
}

// Category is a coloured label a message can carry. It's stored on the
// message as the keyword $cat-<id>, which mail apps keep.
type Category struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// The categories every account starts with, as in Outlook.
var defaultCategories = []Category{
	{"red", "Red", "#d93025"},
	{"orange", "Orange", "#e37400"},
	{"yellow", "Yellow", "#f9ab00"},
	{"green", "Green", "#188038"},
	{"blue", "Blue", "#1a73e8"},
	{"purple", "Purple", "#9334e6"},
}

const catPrefix = "$cat-"

func catFlag(id string) string { return catPrefix + id }

var (
	idRe    = regexp.MustCompile(`^[a-z0-9]{1,16}$`)
	colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

// check makes sure prefs are within bounds, naming the first thing that isn't.
func (p *Prefs) check() error {
	p.Name = strings.TrimSpace(clip(p.Name, 100))
	if len(p.Signatures) > 20 {
		return errors.New("up to 20 signatures")
	}
	seen := map[string]bool{}
	for i := range p.Signatures {
		sig := &p.Signatures[i]
		if !idRe.MatchString(sig.ID) || seen["s"+sig.ID] {
			return fmt.Errorf("signature %q has a bad id", sig.Name)
		}
		seen["s"+sig.ID] = true
		sig.Name = strings.TrimSpace(clip(sig.Name, 100))
		if len(sig.HTML) > 64<<10 {
			return fmt.Errorf("signature %q is over 64 KB", sig.Name)
		}
	}
	for _, id := range []string{p.NewSignature, p.ReplySignature} {
		if id != "" && !seen["s"+id] {
			return errors.New("the chosen signature doesn't exist")
		}
	}
	if len(p.Categories) > 30 {
		return errors.New("up to 30 categories")
	}
	for i := range p.Categories {
		c := &p.Categories[i]
		if !idRe.MatchString(c.ID) || seen["c"+c.ID] {
			return fmt.Errorf("category %q has a bad id", c.Name)
		}
		seen["c"+c.ID] = true
		c.Name = strings.TrimSpace(clip(c.Name, 60))
		if c.Name == "" {
			return errors.New("every category needs a name")
		}
		if !colorRe.MatchString(c.Color) {
			return fmt.Errorf("category %q needs a colour like #1a73e8", c.Name)
		}
	}
	if len(p.Rules) > 100 {
		return errors.New("up to 100 rules")
	}
	for i := range p.Rules {
		if err := p.Rules[i].check(seen); err != nil {
			return err
		}
	}
	return nil
}

// prefsStore keeps each account's prefs in <dir>/<address>.json.
type prefsStore struct {
	dir string
	mu  sync.Mutex
	m   map[string]*Prefs
}

func (ps *prefsStore) path(email string) (string, error) {
	name, err := store.AccountName(email)
	if err != nil {
		return "", err
	}
	return filepath.Join(ps.dir, name+".json"), nil
}

// get returns a copy of email's prefs, with the default categories for an
// account that never set any.
func (ps *prefsStore) get(email string) Prefs {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return clonePrefs(ps.load(email))
}

func (ps *prefsStore) load(email string) *Prefs {
	if p, ok := ps.m[email]; ok {
		return p
	}
	p := &Prefs{}
	if path, err := ps.path(email); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			json.Unmarshal(b, p)
		}
	}
	if p.Categories == nil {
		p.Categories = append([]Category(nil), defaultCategories...)
	}
	if p.Signatures == nil {
		p.Signatures = []Signature{}
	}
	if p.Rules == nil {
		p.Rules = []Rule{}
	}
	ps.m[email] = p
	return p
}

func (ps *prefsStore) set(email string, p Prefs) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path, err := ps.path(email)
	if err != nil {
		return err
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if err := users.WriteFileAtomic(path, b, 0o600); err != nil {
		return err
	}
	c := clonePrefs(&p)
	ps.m[email] = &c
	return nil
}

// update changes email's prefs with fn and saves them.
func (ps *prefsStore) update(email string, fn func(*Prefs)) error {
	p := ps.get(email)
	fn(&p)
	return ps.set(email, p)
}

func clonePrefs(p *Prefs) Prefs {
	c := *p
	c.Signatures = append([]Signature{}, p.Signatures...)
	c.Categories = append([]Category{}, p.Categories...)
	c.Rules = make([]Rule, len(p.Rules))
	for i, r := range p.Rules {
		r.Conditions = append([]Condition{}, r.Conditions...)
		r.Actions = append([]Action{}, r.Actions...)
		c.Rules[i] = r
	}
	return c
}

func (s *Server) putPrefs(w http.ResponseWriter, r *http.Request, a *account) {
	var p Prefs
	if err := readJSON(r, &p, 4<<20); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := p.check(); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.prefs.set(a.email, p); err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, s.prefs.get(a.email))
}
