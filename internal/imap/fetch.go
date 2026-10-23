package imap

import (
	"bytes"
	"fmt"
	"mime"
	"net/mail"
	"sort"
	"strconv"
	"strings"

	"mailer/internal/message"
)

// fetchItem is one requested FETCH attribute.
type fetchItem struct {
	name string // UID, FLAGS, ENVELOPE, BODYSTRUCTURE, BODY, BODYSECTION, RFC822.SIZE, INTERNALDATE

	// For BODYSECTION:
	respName string // response label, e.g. "BODY[HEADER.FIELDS (FROM)]<0>" or "RFC822"
	peek     bool
	path     []int
	spec     string // "", HEADER, HEADER.FIELDS, HEADER.FIELDS.NOT, TEXT, MIME
	fields   []string
	partial  bool
	offset   int
	count    int
}

func (f *fetchItem) needsMessage() bool {
	switch f.name {
	case "ENVELOPE", "BODYSTRUCTURE", "BODY", "BODYSECTION":
		return true
	}
	return false
}

// parseFetchItems parses the FETCH attribute argument (an atom or a list).
func parseFetchItems(t token) ([]*fetchItem, error) {
	var atoms []string
	if t.kind == tList {
		for _, x := range t.list {
			if x.kind != tAtom {
				return nil, errSyntax
			}
			atoms = append(atoms, x.s)
		}
	} else {
		atoms = []string{t.s}
	}
	var items []*fetchItem
	for _, a := range atoms {
		upper := strings.ToUpper(a)
		switch upper {
		case "ALL":
			atoms := []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE"}
			for _, n := range atoms {
				items = append(items, &fetchItem{name: n})
			}
		case "FAST":
			for _, n := range []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE"} {
				items = append(items, &fetchItem{name: n})
			}
		case "FULL":
			for _, n := range []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE", "BODY"} {
				items = append(items, &fetchItem{name: n})
			}
		case "UID", "FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE", "BODYSTRUCTURE", "BODY":
			items = append(items, &fetchItem{name: upper})
		case "RFC822":
			items = append(items, &fetchItem{name: "BODYSECTION", respName: "RFC822"})
		case "RFC822.HEADER":
			items = append(items, &fetchItem{name: "BODYSECTION", respName: "RFC822.HEADER", peek: true, spec: "HEADER"})
		case "RFC822.TEXT":
			items = append(items, &fetchItem{name: "BODYSECTION", respName: "RFC822.TEXT", spec: "TEXT"})
		default:
			it, err := parseBodySection(a)
			if err != nil {
				return nil, err
			}
			items = append(items, it)
		}
	}
	return items, nil
}

// parseBodySection parses BODY[...]<...> and BODY.PEEK[...]<...>.
func parseBodySection(a string) (*fetchItem, error) {
	upper := strings.ToUpper(a)
	it := &fetchItem{name: "BODYSECTION"}
	var rest string
	switch {
	case strings.HasPrefix(upper, "BODY.PEEK["):
		it.peek = true
		rest = a[len("BODY.PEEK["):]
	case strings.HasPrefix(upper, "BODY["):
		rest = a[len("BODY["):]
	default:
		return nil, fmt.Errorf("unknown fetch attribute %q", a)
	}
	end := strings.LastIndexByte(rest, ']')
	if end < 0 {
		return nil, errSyntax
	}
	sec, after := rest[:end], rest[end+1:]

	// Leading part numbers.
	var specText string
	for sec != "" {
		numStr, tail, _ := strings.Cut(sec, ".")
		n, err := strconv.Atoi(numStr)
		if err != nil {
			specText = sec
			break
		}
		if n < 1 {
			return nil, errSyntax
		}
		it.path = append(it.path, n)
		sec = tail
	}
	specUpper := strings.ToUpper(strings.TrimSpace(specText))
	switch {
	case specUpper == "", specUpper == "HEADER", specUpper == "TEXT":
		it.spec = specUpper
	case specUpper == "MIME":
		if len(it.path) == 0 {
			return nil, errSyntax
		}
		it.spec = specUpper
	case strings.HasPrefix(specUpper, "HEADER.FIELDS.NOT"), strings.HasPrefix(specUpper, "HEADER.FIELDS"):
		it.spec = "HEADER.FIELDS"
		listText := specText[len("HEADER.FIELDS"):]
		if strings.HasPrefix(specUpper, "HEADER.FIELDS.NOT") {
			it.spec = "HEADER.FIELDS.NOT"
			listText = specText[len("HEADER.FIELDS.NOT"):]
		}
		toks, err := parseArgs([]byte(strings.TrimSpace(listText)))
		if err != nil || len(toks) != 1 || toks[0].kind != tList {
			return nil, errSyntax
		}
		for _, f := range toks[0].list {
			it.fields = append(it.fields, strings.ToUpper(f.s))
		}
	default:
		return nil, fmt.Errorf("bad section %q", specText)
	}

	if after != "" {
		if !strings.HasPrefix(after, "<") || !strings.HasSuffix(after, ">") {
			return nil, errSyntax
		}
		o, c, ok := strings.Cut(after[1:len(after)-1], ".")
		off, err1 := strconv.Atoi(o)
		cnt, err2 := strconv.Atoi(c)
		if !ok || err1 != nil || err2 != nil || off < 0 || cnt < 1 {
			return nil, errSyntax
		}
		it.partial, it.offset, it.count = true, off, cnt
	}

	// Response label echoes the section without PEEK and with only the
	// partial origin.
	var label strings.Builder
	label.WriteString("BODY[")
	for i, n := range it.path {
		if i > 0 {
			label.WriteByte('.')
		}
		label.WriteString(strconv.Itoa(n))
	}
	if it.spec != "" {
		if len(it.path) > 0 {
			label.WriteByte('.')
		}
		label.WriteString(it.spec)
		if it.fields != nil {
			label.WriteString(" (")
			for i, f := range it.fields {
				if i > 0 {
					label.WriteByte(' ')
				}
				label.WriteString(quote(f))
			}
			label.WriteByte(')')
		}
	}
	label.WriteByte(']')
	if it.partial {
		fmt.Fprintf(&label, "<%d>", it.offset)
	}
	it.respName = label.String()
	return it, nil
}

// child navigates to part number n under p.
func child(p *message.Part, n int) *message.Part {
	if p.Embedded != nil {
		p = p.Embedded
	}
	if p.IsMultipart() && len(p.Children) > 0 {
		if n < 1 || n > len(p.Children) {
			return nil
		}
		return p.Children[n-1]
	}
	if n == 1 {
		return p
	}
	return nil
}

// extractSection returns the bytes for a body section request.
func extractSection(root *message.Part, raw []byte, it *fetchItem) []byte {
	var out []byte
	if len(it.path) == 0 && it.spec == "" {
		out = raw
	} else {
		p := root
		for _, n := range it.path {
			if p = child(p, n); p == nil {
				return nil
			}
		}
		msg := p
		if len(it.path) > 0 && (it.spec == "HEADER" || it.spec == "TEXT" || strings.HasPrefix(it.spec, "HEADER.FIELDS")) {
			if p.Embedded == nil {
				return nil
			}
			msg = p.Embedded
		}
		switch it.spec {
		case "":
			out = p.Body
		case "MIME":
			out = p.HeaderBytes
		case "HEADER":
			out = msg.HeaderBytes
		case "TEXT":
			out = msg.Body
		case "HEADER.FIELDS", "HEADER.FIELDS.NOT":
			want := map[string]bool{}
			for _, f := range it.fields {
				want[f] = true
			}
			var b bytes.Buffer
			for _, f := range msg.Header {
				if want[strings.ToUpper(f.Name)] == (it.spec == "HEADER.FIELDS") {
					b.Write(f.Raw)
				}
			}
			b.WriteString("\r\n")
			out = b.Bytes()
		}
	}
	if it.partial {
		if it.offset >= len(out) {
			return []byte{}
		}
		out = out[it.offset:]
		if it.count < len(out) {
			out = out[:it.count]
		}
	}
	return out
}

// ---- ENVELOPE ----

func writeEnvelope(b *bytes.Buffer, h message.Header) {
	from := h.Get("From")
	sender := h.Get("Sender")
	if sender == "" {
		sender = from
	}
	replyTo := h.Get("Reply-To")
	if replyTo == "" {
		replyTo = from
	}
	b.WriteByte('(')
	b.WriteString(nstring(h.Get("Date")))
	b.WriteByte(' ')
	b.WriteString(nstring(h.Get("Subject")))
	for _, v := range []string{from, sender, replyTo, h.Get("To"), h.Get("Cc"), h.Get("Bcc")} {
		b.WriteByte(' ')
		writeAddressList(b, v)
	}
	b.WriteByte(' ')
	b.WriteString(nstring(h.Get("In-Reply-To")))
	b.WriteByte(' ')
	b.WriteString(nstring(h.Get("Message-Id")))
	b.WriteByte(')')
}

var addrParser = &mail.AddressParser{WordDecoder: &mime.WordDecoder{}}

func writeAddressList(b *bytes.Buffer, v string) {
	if strings.TrimSpace(v) == "" {
		b.WriteString("NIL")
		return
	}
	list, err := addrParser.ParseList(v)
	if err != nil || len(list) == 0 {
		b.WriteString("NIL")
		return
	}
	b.WriteByte('(')
	for _, a := range list {
		local, domain := a.Address, ""
		if i := strings.LastIndexByte(a.Address, '@'); i >= 0 {
			local, domain = a.Address[:i], a.Address[i+1:]
		}
		name := a.Name
		if !isASCII(name) {
			name = mime.QEncoding.Encode("utf-8", name)
		}
		fmt.Fprintf(b, "(%s NIL %s %s)", nstring(name), nstring(local), nstring(domain))
	}
	b.WriteByte(')')
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// ---- BODYSTRUCTURE ----

func writeBodyStructure(b *bytes.Buffer, p *message.Part, ext bool) {
	if p.IsMultipart() && len(p.Children) > 0 {
		b.WriteByte('(')
		for _, c := range p.Children {
			writeBodyStructure(b, c, ext)
		}
		b.WriteByte(' ')
		b.WriteString(quote(strings.ToUpper(p.SubType)))
		if ext {
			b.WriteByte(' ')
			writeParams(b, p.Params)
			b.WriteByte(' ')
			writeDisposition(b, p.Header.Get("Content-Disposition"))
			b.WriteByte(' ')
			b.WriteString(nstring(p.Header.Get("Content-Language")))
			b.WriteByte(' ')
			b.WriteString(nstring(p.Header.Get("Content-Location")))
		}
		b.WriteByte(')')
		return
	}

	typ, sub, params := p.Type, p.SubType, p.Params
	if p.IsMultipart() { // multipart with no parseable children
		typ, sub, params = "text", "plain", map[string]string{"charset": "us-ascii"}
	}
	enc := strings.ToUpper(p.Header.Get("Content-Transfer-Encoding"))
	if enc == "" {
		enc = "7BIT"
	}
	fmt.Fprintf(b, "(%s %s ", quote(strings.ToUpper(typ)), quote(strings.ToUpper(sub)))
	writeParams(b, params)
	fmt.Fprintf(b, " %s %s %s %d",
		nstring(p.Header.Get("Content-Id")),
		nstring(p.Header.Get("Content-Description")),
		quote(enc), len(p.Body))
	switch {
	case typ == "text":
		fmt.Fprintf(b, " %d", message.Lines(p.Body))
	case typ == "message" && sub == "rfc822" && p.Embedded != nil:
		b.WriteByte(' ')
		writeEnvelope(b, p.Embedded.Header)
		b.WriteByte(' ')
		writeBodyStructure(b, p.Embedded, ext)
		fmt.Fprintf(b, " %d", message.Lines(p.Body))
	}
	if ext {
		b.WriteByte(' ')
		b.WriteString(nstring(p.Header.Get("Content-Md5")))
		b.WriteByte(' ')
		writeDisposition(b, p.Header.Get("Content-Disposition"))
		b.WriteByte(' ')
		b.WriteString(nstring(p.Header.Get("Content-Language")))
		b.WriteByte(' ')
		b.WriteString(nstring(p.Header.Get("Content-Location")))
	}
	b.WriteByte(')')
}

func writeParams(b *bytes.Buffer, params map[string]string) {
	if len(params) == 0 {
		b.WriteString("NIL")
		return
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b.WriteByte('(')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		v := params[k]
		if !isASCII(v) {
			v = mime.BEncoding.Encode("utf-8", v)
		}
		fmt.Fprintf(b, "%s %s", quote(strings.ToUpper(k)), quote(v))
	}
	b.WriteByte(')')
}

func writeDisposition(b *bytes.Buffer, v string) {
	if v == "" {
		b.WriteString("NIL")
		return
	}
	d, params, err := mime.ParseMediaType(v)
	if err != nil {
		// Still report the disposition type so clients show attachments.
		d, _, _ = strings.Cut(v, ";")
		d = strings.TrimSpace(strings.ToLower(d))
		params = nil
	}
	if d == "" {
		b.WriteString("NIL")
		return
	}
	fmt.Fprintf(b, "(%s ", quote(strings.ToUpper(d)))
	writeParams(b, params)
	b.WriteByte(')')
}
