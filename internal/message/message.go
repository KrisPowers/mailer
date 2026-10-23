// Package message parses RFC 5322 / MIME messages into a tree of parts while
// keeping byte-exact access to every header field and body, which IMAP FETCH
// and DKIM both require.
package message

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/quotedprintable"
	"strings"
)

// Field is a single header field.
type Field struct {
	Name  string // as written, e.g. "Content-Type"
	Value string // unfolded value with leading whitespace trimmed
	Raw   []byte // exact bytes including the trailing CRLF and any folding
}

// Header is an ordered list of fields.
type Header []Field

// Get returns the value of the first field named name (case-insensitive).
func (h Header) Get(name string) string {
	for _, f := range h {
		if strings.EqualFold(f.Name, name) {
			return f.Value
		}
	}
	return ""
}

// Has reports whether a field named name exists.
func (h Header) Has(name string) bool {
	for _, f := range h {
		if strings.EqualFold(f.Name, name) {
			return true
		}
	}
	return false
}

// Part is a node in the MIME tree. The root Part is the whole message.
type Part struct {
	Header      Header
	HeaderBytes []byte // raw header including the terminating blank line
	Body        []byte // raw body (everything after the blank line)

	Type, SubType string            // lowercased, e.g. "text", "plain"
	Params        map[string]string // Content-Type parameters (keys lowercased)

	Children []*Part // for multipart/*
	Embedded *Part   // for message/rfc822
}

// IsMultipart reports whether p is multipart/*.
func (p *Part) IsMultipart() bool { return p.Type == "multipart" }

// Parse parses a full message. Line endings are expected to be CRLF; use
// NormalizeCRLF on untrusted input first.
func Parse(raw []byte) *Part {
	return parsePart(raw, "text/plain", 0)
}

const maxDepth = 32

func parsePart(raw []byte, defaultType string, depth int) *Part {
	p := &Part{}
	p.HeaderBytes, p.Body = SplitHeader(raw)
	p.Header = ParseHeader(p.HeaderBytes)

	ct := p.Header.Get("Content-Type")
	mt, params, err := mime.ParseMediaType(ct)
	if ct == "" || err != nil || !strings.Contains(mt, "/") {
		mt, params, _ = mime.ParseMediaType(defaultType)
		if defaultType == "text/plain" {
			params = map[string]string{"charset": "us-ascii"}
		}
	}
	p.Type, p.SubType, _ = strings.Cut(mt, "/")
	p.Params = params
	if p.Params == nil {
		p.Params = map[string]string{}
	}
	if depth >= maxDepth {
		return p
	}

	switch {
	case p.Type == "multipart" && p.Params["boundary"] != "":
		childDefault := "text/plain"
		if p.SubType == "digest" {
			childDefault = "message/rfc822"
		}
		for _, body := range splitMultipart(p.Body, p.Params["boundary"]) {
			p.Children = append(p.Children, parsePart(body, childDefault, depth+1))
		}
	case p.Type == "message" && p.SubType == "rfc822":
		p.Embedded = parsePart(p.Body, "text/plain", depth+1)
	}
	return p
}

// SplitHeader splits raw at the first blank line. The returned header
// includes the blank line itself.
func SplitHeader(raw []byte) (header, body []byte) {
	if bytes.HasPrefix(raw, []byte("\r\n")) {
		return raw[:2], raw[2:]
	}
	if i := bytes.Index(raw, []byte("\r\n\r\n")); i >= 0 {
		return raw[:i+4], raw[i+4:]
	}
	return raw, nil
}

// ParseHeader parses raw header bytes into fields.
func ParseHeader(hdr []byte) Header {
	var h Header
	rest := hdr
	for len(rest) > 0 {
		// Find the end of this field: a CRLF not followed by WSP.
		end := 0
		for {
			i := bytes.Index(rest[end:], []byte("\r\n"))
			if i < 0 {
				end = len(rest)
				break
			}
			end += i + 2
			if end >= len(rest) || (rest[end] != ' ' && rest[end] != '\t') {
				break
			}
		}
		raw := rest[:end]
		rest = rest[end:]
		if bytes.Equal(raw, []byte("\r\n")) {
			break // blank line terminating the header
		}
		name, value, ok := bytes.Cut(raw, []byte(":"))
		if !ok {
			continue
		}
		h = append(h, Field{
			Name:  strings.TrimRight(string(name), " \t"),
			Value: strings.TrimSpace(unfold(string(value))),
			Raw:   raw,
		})
	}
	return h
}

func unfold(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "")
	return strings.ReplaceAll(s, "\n", "")
}

// splitMultipart returns the bodies between boundary delimiters, excluding
// preamble and epilogue.
func splitMultipart(body []byte, boundary string) [][]byte {
	delim := []byte("--" + boundary)
	var parts [][]byte
	start := -1 // start offset of the current part, -1 while in preamble
	off := 0
	for off <= len(body) {
		lineEnd := bytes.Index(body[off:], []byte("\r\n"))
		next := 0
		var line []byte
		if lineEnd < 0 {
			line = body[off:]
			next = len(body) + 1
		} else {
			line = body[off : off+lineEnd]
			next = off + lineEnd + 2
		}
		if bytes.HasPrefix(line, delim) {
			rest := line[len(delim):]
			closing := bytes.HasPrefix(rest, []byte("--"))
			if closing || len(bytes.TrimRight(rest, " \t")) == 0 {
				if start >= 0 {
					end := off - 2 // the CRLF before the delimiter belongs to it
					if end < start {
						end = start
					}
					parts = append(parts, body[start:end])
				}
				if closing {
					return parts
				}
				start = min(next, len(body))
			}
		}
		off = next
	}
	if start >= 0 && start < len(body) {
		parts = append(parts, body[start:]) // unterminated multipart
	}
	return parts
}

// Lines counts the number of lines in b, as IMAP BODYSTRUCTURE reports them.
func Lines(b []byte) int {
	n := bytes.Count(b, []byte("\n"))
	if len(b) > 0 && b[len(b)-1] != '\n' {
		n++
	}
	return n
}

// NormalizeCRLF converts bare LF and bare CR line endings to CRLF.
func NormalizeCRLF(b []byte) []byte {
	if !bytes.ContainsAny(b, "\r\n") {
		return b
	}
	out := make([]byte, 0, len(b)+len(b)/50)
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch c {
		case '\r':
			out = append(out, '\r', '\n')
			if i+1 < len(b) && b[i+1] == '\n' {
				i++
			}
		case '\n':
			out = append(out, '\r', '\n')
		default:
			out = append(out, c)
		}
	}
	return out
}

// DecodedBody returns the body with its Content-Transfer-Encoding removed.
func (p *Part) DecodedBody() []byte {
	var r io.Reader
	switch strings.ToLower(p.Header.Get("Content-Transfer-Encoding")) {
	case "base64":
		clean := bytes.Map(func(r rune) rune {
			if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, p.Body)
		// Strip padding and decode unpadded, which tolerates missing padding.
		r = base64.NewDecoder(base64.RawStdEncoding, bytes.NewReader(bytes.TrimRight(clean, "=")))
	case "quoted-printable":
		r = quotedprintable.NewReader(bytes.NewReader(p.Body))
	default:
		return p.Body
	}
	out, _ := io.ReadAll(r)
	return out
}

// Walk calls fn for p and every descendant (including embedded messages).
func (p *Part) Walk(fn func(*Part)) {
	fn(p)
	for _, c := range p.Children {
		c.Walk(fn)
	}
	if p.Embedded != nil {
		p.Embedded.Walk(fn)
	}
}

var wordDecoder = &mime.WordDecoder{
	CharsetReader: func(charset string, input io.Reader) (io.Reader, error) {
		// Without third-party charset tables, pass bytes through; UTF-8 and
		// ASCII (the vast majority) decode correctly, and latin-1 is mapped.
		switch strings.ToLower(charset) {
		case "iso-8859-1", "latin1", "windows-1252":
			b, err := io.ReadAll(input)
			if err != nil {
				return nil, err
			}
			rs := make([]rune, len(b))
			for i, c := range b {
				rs[i] = rune(c)
			}
			return strings.NewReader(string(rs)), nil
		}
		return input, nil
	},
}

// DecodeHeader decodes RFC 2047 encoded-words in a header value.
func DecodeHeader(s string) string {
	d, err := wordDecoder.DecodeHeader(s)
	if err != nil {
		return s
	}
	return d
}
