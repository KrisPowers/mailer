package imap

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type tokKind int

const (
	tAtom tokKind = iota
	tString
	tList
)

// token is a parsed IMAP argument: an atom, a string (quoted or literal),
// or a parenthesized list.
type token struct {
	kind tokKind
	s    string
	list []token
}

func (t token) isNil() bool { return t.kind == tAtom && strings.EqualFold(t.s, "NIL") }

var errSyntax = errors.New("syntax error")

type lexer struct {
	b []byte
	i int
}

// parseArgs parses everything remaining in b as a flat list of tokens.
func parseArgs(b []byte) ([]token, error) {
	l := &lexer{b: b}
	toks, err := l.parseUntil(0)
	if err != nil {
		return nil, err
	}
	return toks, nil
}

// parseUntil reads tokens until end of input (end==0) or a ')' (end==')').
func (l *lexer) parseUntil(end byte) ([]token, error) {
	var toks []token
	for {
		for l.i < len(l.b) && l.b[l.i] == ' ' {
			l.i++
		}
		if l.i >= len(l.b) {
			if end != 0 {
				return nil, errSyntax
			}
			return toks, nil
		}
		c := l.b[l.i]
		switch {
		case c == ')':
			if end != ')' {
				return nil, errSyntax
			}
			l.i++
			return toks, nil
		case c == '(':
			l.i++
			sub, err := l.parseUntil(')')
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tList, list: sub})
		case c == '"':
			s, err := l.quoted()
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tString, s: s})
		case c == '{' || (c == '~' && l.i+1 < len(l.b) && l.b[l.i+1] == '{'):
			s, err := l.literal()
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind: tString, s: s})
		default:
			toks = append(toks, token{kind: tAtom, s: l.atom()})
		}
	}
}

func (l *lexer) quoted() (string, error) {
	l.i++ // opening quote
	var sb strings.Builder
	for l.i < len(l.b) {
		c := l.b[l.i]
		l.i++
		switch c {
		case '"':
			return sb.String(), nil
		case '\\':
			if l.i >= len(l.b) {
				return "", errSyntax
			}
			sb.WriteByte(l.b[l.i])
			l.i++
		case '\r', '\n':
			return "", errSyntax
		default:
			sb.WriteByte(c)
		}
	}
	return "", errSyntax
}

func (l *lexer) literal() (string, error) {
	if l.b[l.i] == '~' {
		l.i++
	}
	close := bytes.IndexByte(l.b[l.i:], '}')
	if close < 0 {
		return "", errSyntax
	}
	spec := strings.TrimSuffix(string(l.b[l.i+1:l.i+close]), "+")
	n, err := strconv.Atoi(spec)
	if err != nil || n < 0 {
		return "", errSyntax
	}
	l.i += close + 1
	if !bytes.HasPrefix(l.b[l.i:], []byte("\r\n")) {
		return "", errSyntax
	}
	l.i += 2
	if n > len(l.b)-l.i {
		return "", errSyntax
	}
	s := string(l.b[l.i : l.i+n])
	l.i += n
	return s, nil
}

// atom reads an atom. Square brackets are included verbatim with anything
// inside them (spaces and parentheses too), so a fetch attribute such as
// BODY.PEEK[HEADER.FIELDS (FROM TO)]<0.100> is a single atom.
func (l *lexer) atom() string {
	start := l.i
	depth := 0
	for l.i < len(l.b) {
		c := l.b[l.i]
		if depth == 0 && (c == ' ' || c == '(' || c == ')' || c == '"' || c == '{') {
			break
		}
		switch c {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		}
		l.i++
	}
	return string(l.b[start:l.i])
}

// literalSuffix reports whether line ends in a literal announcement
// "{n}" or "{n+}", returning n and whether it is non-synchronizing.
func literalSuffix(line []byte) (n int64, nonSync, ok bool) {
	if !bytes.HasSuffix(line, []byte("}")) {
		return 0, false, false
	}
	open := bytes.LastIndexByte(line, '{')
	if open < 0 {
		return 0, false, false
	}
	spec := string(line[open+1 : len(line)-1])
	if strings.HasSuffix(spec, "+") {
		nonSync = true
		spec = spec[:len(spec)-1]
	}
	n, err := strconv.ParseInt(spec, 10, 64)
	if err != nil || n < 0 {
		return 0, false, false
	}
	return n, nonSync, true
}

// ---- output encoding ----

// quote encodes s as an IMAP string, using a literal when needed.
func quote(s string) string {
	needLiteral := len(s) > 1024
	for i := 0; i < len(s) && !needLiteral; i++ {
		if c := s[i]; c == '\r' || c == '\n' || c == 0 || c >= 0x80 {
			needLiteral = true
		}
	}
	if needLiteral {
		return fmt.Sprintf("{%d}\r\n%s", len(s), s)
	}
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

func nstring(s string) string {
	if s == "" {
		return "NIL"
	}
	return quote(s)
}

func literalBytes(b []byte) string {
	return fmt.Sprintf("{%d}\r\n%s", len(b), b)
}

// ---- sequence sets ----

type seqRange struct{ lo, hi uint32 } // 0 stands for '*'

type seqSet []seqRange

func parseSeqSet(s string) (seqSet, error) {
	if s == "" {
		return nil, errSyntax
	}
	var set seqSet
	for _, part := range strings.Split(s, ",") {
		lo, hi, isRange := strings.Cut(part, ":")
		a, err := parseSeqNum(lo)
		if err != nil {
			return nil, err
		}
		b := a
		if isRange {
			if b, err = parseSeqNum(hi); err != nil {
				return nil, err
			}
		}
		set = append(set, seqRange{a, b})
	}
	return set, nil
}

func parseSeqNum(s string) (uint32, error) {
	if s == "*" {
		return 0, nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil || n == 0 {
		return 0, errSyntax
	}
	return uint32(n), nil
}

// contains reports whether n is in the set, where '*' means max.
func (set seqSet) contains(n, max uint32) bool {
	for _, r := range set {
		lo, hi := r.lo, r.hi
		if lo == 0 {
			lo = max
		}
		if hi == 0 {
			hi = max
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		if n >= lo && n <= hi {
			return true
		}
	}
	return false
}

func isSeqSet(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9') && c != ':' && c != ',' && c != '*' {
			return false
		}
	}
	return true
}

// formatUIDSet compresses a list of UIDs into IMAP set syntax.
func formatUIDSet(uids []uint32) string {
	if len(uids) == 0 {
		return ""
	}
	sorted := append([]uint32(nil), uids...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sb strings.Builder
	start, prev := sorted[0], sorted[0]
	flush := func() {
		if sb.Len() > 0 {
			sb.WriteByte(',')
		}
		if start == prev {
			fmt.Fprintf(&sb, "%d", start)
		} else {
			fmt.Fprintf(&sb, "%d:%d", start, prev)
		}
	}
	for _, u := range sorted[1:] {
		if u == prev+1 {
			prev = u
			continue
		}
		flush()
		start, prev = u, u
	}
	flush()
	return sb.String()
}

// formatUIDSetOrdered keeps the given order, which COPYUID requires so that
// source and destination sets correspond element by element.
func formatUIDSetOrdered(uids []uint32) string {
	parts := make([]string, len(uids))
	for i, u := range uids {
		parts[i] = strconv.FormatUint(uint64(u), 10)
	}
	return strings.Join(parts, ",")
}

// matchPattern implements LIST wildcards: '*' matches anything, '%' matches
// anything except the hierarchy delimiter.
func matchPattern(pattern, name string) bool {
	if pattern == "" {
		return name == ""
	}
	switch pattern[0] {
	case '*':
		for i := 0; i <= len(name); i++ {
			if matchPattern(pattern[1:], name[i:]) {
				return true
			}
		}
		return false
	case '%':
		for i := 0; i <= len(name); i++ {
			if matchPattern(pattern[1:], name[i:]) {
				return true
			}
			if i < len(name) && name[i] == '/' {
				return false
			}
		}
		return false
	}
	if name == "" || pattern[0] != name[0] {
		return false
	}
	return matchPattern(pattern[1:], name[1:])
}
