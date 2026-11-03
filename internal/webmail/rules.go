package webmail

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"mailer/internal/store"
)

// Rule files incoming mail by what it says, as Outlook's rules do: when its
// conditions match, all of them or any one, its actions run.
type Rule struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	On         bool        `json:"on"`
	Any        bool        `json:"any"`
	Conditions []Condition `json:"conditions"`
	Actions    []Action    `json:"actions"`
	// Stop keeps later rules from running on a message this one matched.
	Stop bool `json:"stop"`
}

// Condition is one test on a message. Field is from, to (or cc), subject,
// body, anywhere (any of those), attachment or marketing; the last two take
// no value. Op is contains, not_contains, is, starts or ends.
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// Action is what a matched rule does: move or copy (to the folder in
// Value), read, flag, category (the category id in Value) or delete.
type Action struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

var (
	textFields = []string{"from", "to", "subject", "body", "anywhere"}
	ops        = []string{"contains", "not_contains", "is", "starts", "ends"}
	kinds      = []string{"move", "copy", "read", "flag", "category", "delete"}
)

func (r *Rule) check(seen map[string]bool) error {
	if !idRe.MatchString(r.ID) || seen["r"+r.ID] {
		return fmt.Errorf("rule %q has a bad id", r.Name)
	}
	seen["r"+r.ID] = true
	r.Name = strings.TrimSpace(clip(r.Name, 100))
	if r.Name == "" {
		return errors.New("every rule needs a name")
	}
	if len(r.Conditions) == 0 || len(r.Conditions) > 10 {
		return fmt.Errorf("rule %q needs one to ten conditions", r.Name)
	}
	for i := range r.Conditions {
		c := &r.Conditions[i]
		c.Value = clip(c.Value, 500)
		switch {
		case c.Field == "attachment" || c.Field == "marketing":
		case !slices.Contains(textFields, c.Field):
			return fmt.Errorf("rule %q tests something unknown", r.Name)
		case !slices.Contains(ops, c.Op):
			return fmt.Errorf("rule %q has an unknown test", r.Name)
		case strings.TrimSpace(c.Value) == "":
			return fmt.Errorf("rule %q has a test with nothing to look for", r.Name)
		}
	}
	if len(r.Actions) == 0 || len(r.Actions) > 10 {
		return fmt.Errorf("rule %q needs one to ten actions", r.Name)
	}
	for i := range r.Actions {
		a := &r.Actions[i]
		a.Value = clip(a.Value, 255)
		switch {
		case !slices.Contains(kinds, a.Kind):
			return fmt.Errorf("rule %q does something unknown", r.Name)
		case (a.Kind == "move" || a.Kind == "copy") && a.Value == "":
			return fmt.Errorf("rule %q needs a folder to file into", r.Name)
		case a.Kind == "category" && !seen["c"+a.Value]:
			return fmt.Errorf("rule %q adds a category that doesn't exist", r.Name)
		}
	}
	return nil
}

func test(have, op, want string) bool {
	have, want = strings.ToLower(have), strings.ToLower(strings.TrimSpace(want))
	switch op {
	case "contains":
		return strings.Contains(have, want)
	case "not_contains":
		return !strings.Contains(have, want)
	case "is":
		return strings.TrimSpace(have) == want
	case "starts":
		return strings.HasPrefix(strings.TrimSpace(have), want)
	case "ends":
		return strings.HasSuffix(strings.TrimSpace(have), want)
	}
	return false
}

func who(list ...Addr) []string {
	out := make([]string, 0, len(list)*2)
	for _, a := range list {
		out = append(out, a.Email, a.Name+" <"+a.Email+">")
	}
	return out
}

func (c Condition) match(p *parsed) bool {
	var values []string
	switch c.Field {
	case "attachment":
		return slices.ContainsFunc(p.Attachments, func(a Attachment) bool { return !a.Inline })
	case "marketing":
		return p.Marketing
	case "from":
		values = who(p.From)
	case "to":
		values = who(append(append([]Addr{}, p.To...), p.Cc...)...)
	case "subject":
		values = []string{p.Subject}
	case "body":
		values = []string{p.Text}
	case "anywhere":
		values = append(append(who(p.From), who(append(append([]Addr{}, p.To...), p.Cc...)...)...), p.Subject, p.Text)
	}
	if c.Op == "not_contains" {
		// None of them may contain it.
		for _, v := range values {
			if !test(v, c.Op, c.Value) {
				return false
			}
		}
		return true
	}
	for _, v := range values {
		if test(v, c.Op, c.Value) {
			return true
		}
	}
	return false
}

func (r *Rule) match(p *parsed) bool {
	for _, c := range r.Conditions {
		if c.match(p) == r.Any {
			return r.Any
		}
	}
	return !r.Any
}

// outcome is what an account's rules do with a message.
type outcome struct {
	folder  string // "" stays where it is
	flags   []string
	copies  []string
	matched bool
}

// apply runs the rules on p in order. trash is the account's Trash, for
// delete.
func apply(rules []Rule, p *parsed, trash string) outcome {
	var o outcome
	for i := range rules {
		r := &rules[i]
		if !r.On || !r.match(p) {
			continue
		}
		o.matched = true
		for _, a := range r.Actions {
			switch a.Kind {
			case "move":
				o.folder = a.Value
			case "copy":
				if !slices.Contains(o.copies, a.Value) {
					o.copies = append(o.copies, a.Value)
				}
			case "read":
				o.flags = append(o.flags, store.FlagSeen)
			case "flag":
				o.flags = append(o.flags, store.FlagFlagged)
			case "category":
				o.flags = append(o.flags, catFlag(a.Value))
			case "delete":
				if trash != "" {
					o.folder = trash
				}
			}
		}
		if r.Stop {
			break
		}
	}
	return o
}

// Sort is the delivery hook for mta.MTA.Rules: it files a message arriving
// for user by the account's rules.
func (s *Server) Sort(user string, data []byte) (folder string, flags []string, copies []string) {
	s.init()
	rules := s.prefs.get(user).Rules
	if !slices.ContainsFunc(rules, func(r Rule) bool { return r.On }) {
		return "", nil, nil
	}
	box, err := s.Store.Account(user)
	if err != nil {
		return "", nil, nil
	}
	o := apply(rules, parse(data), box.FindSpecialUse(`\Trash`))
	exists := func(name string) bool {
		_, err := box.Status(name)
		return err == nil
	}
	if o.folder != "" && !exists(o.folder) {
		o.folder = ""
	}
	for _, c := range o.copies {
		if exists(c) {
			copies = append(copies, c)
		}
	}
	return o.folder, o.flags, copies
}

// runRules runs the rules now on every message in a folder, as if each had
// just arrived.
func (s *Server) runRules(w http.ResponseWriter, r *http.Request, a *account) {
	var req struct {
		Folder string `json:"folder"`
	}
	if err := readJSON(r, &req, 1<<12); err != nil {
		writeErr(w, 400, err)
		return
	}
	snap, err := a.box.Snapshot(req.Folder)
	if err != nil {
		writeErr(w, 404, folderErr(err))
		return
	}
	rules := s.prefs.get(a.email).Rules
	trash := special(a, `\Trash`)
	moved := map[string][]uint32{}
	changed := 0
	for _, m := range snap.Messages {
		p := s.parsedOf(a, snap.Name, snap.UIDValidity, m)
		o := apply(rules, p, trash)
		if !o.matched {
			continue
		}
		changed++
		if len(o.flags) > 0 {
			a.box.SetFlags(snap.Name, []uint32{m.UID}, store.FlagsAdd, o.flags)
		}
		for _, c := range o.copies {
			if c != snap.Name {
				a.box.Copy(snap.Name, c, []uint32{m.UID})
			}
		}
		if o.folder != "" && o.folder != snap.Name {
			moved[o.folder] = append(moved[o.folder], m.UID)
		}
	}
	for to, uids := range moved {
		a.box.Move(snap.Name, to, uids)
	}
	writeJSON(w, map[string]int{"matched": changed})
}
