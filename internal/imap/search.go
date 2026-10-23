package imap

import (
	"bytes"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"mailer/internal/message"
	"mailer/internal/store"
)

type searchCtx struct {
	seq, maxSeq, maxUID uint32
	info                *store.MessageInfo
	load                func() ([]byte, error)

	loaded bool
	part   *message.Part
}

func (c *searchCtx) msg() *message.Part {
	if !c.loaded {
		c.loaded = true
		if raw, err := c.load(); err == nil {
			c.part = message.Parse(raw)
		}
	}
	return c.part
}

type matcher func(*searchCtx) bool

func parseSearch(toks []token) (matcher, error) {
	i := 0
	if i < len(toks) && strings.EqualFold(toks[i].s, "CHARSET") && toks[i].kind == tAtom {
		i += 2 // charset is ignored; values are compared as UTF-8
	}
	var ms []matcher
	for i < len(toks) {
		m, err := parseSearchKey(toks, &i)
		if err != nil {
			return nil, err
		}
		ms = append(ms, m)
	}
	return and(ms), nil
}

func and(ms []matcher) matcher {
	return func(c *searchCtx) bool {
		for _, m := range ms {
			if !m(c) {
				return false
			}
		}
		return true
	}
}

func parseSearchKey(toks []token, i *int) (matcher, error) {
	if *i >= len(toks) {
		return nil, errSyntax
	}
	t := toks[*i]
	*i++
	if t.kind == tList {
		return parseSearch(t.list)
	}
	arg := func() (string, error) {
		if *i >= len(toks) || toks[*i].kind == tList {
			return "", errSyntax
		}
		*i++
		return toks[*i-1].s, nil
	}
	flag := func(f string, want bool) matcher {
		return func(c *searchCtx) bool { return c.info.HasFlag(f) == want }
	}
	key := strings.ToUpper(t.s)
	switch key {
	case "ALL", "OLD":
		return func(*searchCtx) bool { return true }, nil
	case "NEW", "RECENT":
		return func(*searchCtx) bool { return false }, nil
	case "ANSWERED":
		return flag(store.FlagAnswered, true), nil
	case "UNANSWERED":
		return flag(store.FlagAnswered, false), nil
	case "DELETED":
		return flag(store.FlagDeleted, true), nil
	case "UNDELETED":
		return flag(store.FlagDeleted, false), nil
	case "DRAFT":
		return flag(store.FlagDraft, true), nil
	case "UNDRAFT":
		return flag(store.FlagDraft, false), nil
	case "FLAGGED":
		return flag(store.FlagFlagged, true), nil
	case "UNFLAGGED":
		return flag(store.FlagFlagged, false), nil
	case "SEEN":
		return flag(store.FlagSeen, true), nil
	case "UNSEEN":
		return flag(store.FlagSeen, false), nil
	case "KEYWORD", "UNKEYWORD":
		k, err := arg()
		if err != nil {
			return nil, err
		}
		return flag(k, key == "KEYWORD"), nil
	case "FROM", "TO", "CC", "BCC", "SUBJECT":
		v, err := arg()
		if err != nil {
			return nil, err
		}
		return headerContains(key, v), nil
	case "HEADER":
		name, err := arg()
		if err != nil {
			return nil, err
		}
		v, err := arg()
		if err != nil {
			return nil, err
		}
		return headerContains(name, v), nil
	case "BODY", "TEXT":
		v, err := arg()
		if err != nil {
			return nil, err
		}
		needle := strings.ToLower(v)
		withHeader := key == "TEXT"
		return func(c *searchCtx) bool {
			p := c.msg()
			if p == nil {
				return false
			}
			if withHeader && strings.Contains(strings.ToLower(message.DecodeHeader(string(p.HeaderBytes))), needle) {
				return true
			}
			found := false
			p.Walk(func(q *message.Part) {
				if !found && q.Type == "text" {
					found = bytes.Contains(bytes.ToLower(q.DecodedBody()), []byte(needle))
				}
			})
			return found
		}, nil
	case "BEFORE", "ON", "SINCE", "SENTBEFORE", "SENTON", "SENTSINCE":
		v, err := arg()
		if err != nil {
			return nil, err
		}
		d, err := time.Parse("2-Jan-2006", v)
		if err != nil {
			return nil, fmt.Errorf("bad date %q", v)
		}
		sent := strings.HasPrefix(key, "SENT")
		cmp := strings.TrimPrefix(key, "SENT")
		return func(c *searchCtx) bool {
			t := c.info.Date
			if sent {
				p := c.msg()
				if p == nil {
					return false
				}
				st, err := mail.ParseDate(p.Header.Get("Date"))
				if err != nil {
					return false
				}
				t = st
			}
			day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
			switch cmp {
			case "BEFORE":
				return day.Before(d)
			case "ON":
				return day.Equal(d)
			default:
				return !day.Before(d)
			}
		}, nil
	case "LARGER", "SMALLER":
		v, err := arg()
		if err != nil {
			return nil, err
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, errSyntax
		}
		if key == "LARGER" {
			return func(c *searchCtx) bool { return c.info.Size > n }, nil
		}
		return func(c *searchCtx) bool { return c.info.Size < n }, nil
	case "UID":
		v, err := arg()
		if err != nil {
			return nil, err
		}
		set, err := parseSeqSet(v)
		if err != nil {
			return nil, err
		}
		return func(c *searchCtx) bool { return set.contains(c.info.UID, c.maxUID) }, nil
	case "NOT":
		m, err := parseSearchKey(toks, i)
		if err != nil {
			return nil, err
		}
		return func(c *searchCtx) bool { return !m(c) }, nil
	case "OR":
		a, err := parseSearchKey(toks, i)
		if err != nil {
			return nil, err
		}
		b, err := parseSearchKey(toks, i)
		if err != nil {
			return nil, err
		}
		return func(c *searchCtx) bool { return a(c) || b(c) }, nil
	}
	if isSeqSet(t.s) {
		set, err := parseSeqSet(t.s)
		if err != nil {
			return nil, err
		}
		return func(c *searchCtx) bool { return set.contains(c.seq, c.maxSeq) }, nil
	}
	return nil, fmt.Errorf("unknown search key %q", t.s)
}

func headerContains(name, value string) matcher {
	needle := strings.ToLower(value)
	return func(c *searchCtx) bool {
		p := c.msg()
		if p == nil {
			return false
		}
		for _, f := range p.Header {
			if strings.EqualFold(f.Name, name) {
				if needle == "" || strings.Contains(strings.ToLower(message.DecodeHeader(f.Value)), needle) {
					return true
				}
			}
		}
		return false
	}
}
