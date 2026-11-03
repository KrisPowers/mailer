package webmail

import (
	"html"
	"mime"
	"net/mail"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"mailer/internal/message"
)

// Addr is a name and an address, as a message's headers give them.
type Addr struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

// Attachment is a part of a message that isn't its text: a file, or a
// picture the text shows (Inline).
type Attachment struct {
	Index  int    `json:"index"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int    `json:"size"`
	Inline bool   `json:"inline,omitempty"`
}

// parsed is what's read from a stored message once and kept: the fields
// lists show and rules match, and where its parts are.
type parsed struct {
	From        Addr
	To, Cc, Bcc []Addr
	ReplyTo     []Addr
	Subject     string
	Sent        time.Time
	MessageID   string
	InReplyTo   string
	References  string
	Snippet     string
	Attachments []Attachment
	Marketing   bool
	// Text is the message's plain text, or its HTML as text, cut to 64 KB
	// for rules and quoting.
	Text    string
	HasHTML bool
	Remote  bool // the HTML loads pictures from the internet
}

var (
	tagRe    = regexp.MustCompile(`(?s)<[^>]*>`)
	blockRe  = regexp.MustCompile(`(?is)<(style|script|head|title)\b.*?</(style|script|head|title)\s*>`)
	breakRe  = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/li|/h[1-6])\b[^>]*>`)
	spaceRe  = regexp.MustCompile(`[ \t\r\f\v\x{a0}]+`)
	linesRe  = regexp.MustCompile(`\n{3,}`)
	remoteRe = regexp.MustCompile(`(?i)(<img\b[^>]*\bsrc\s*=\s*["']?\s*https?:|url\(\s*["']?\s*https?:|\bbackground\s*=\s*["']?\s*https?:)`)
)

// leaves are a message's parts in order, without the multipart containers.
// A forwarded message/rfc822 part is one leaf: it's an attachment here.
func leaves(root *message.Part) []*message.Part {
	var out []*message.Part
	var walk func(p *message.Part)
	walk = func(p *message.Part) {
		if p.IsMultipart() && len(p.Children) > 0 {
			for _, c := range p.Children {
				walk(c)
			}
			return
		}
		out = append(out, p)
	}
	walk(root)
	return out
}

func disposition(p *message.Part) (string, string) {
	name := p.Params["name"]
	disp, params, err := mime.ParseMediaType(p.Header.Get("Content-Disposition"))
	if err == nil && params["filename"] != "" {
		name = params["filename"]
	}
	return strings.ToLower(disp), strings.TrimSpace(message.DecodeHeader(name))
}

// isBody reports whether p is the message's text rather than an attachment.
func isBody(p *message.Part) bool {
	disp, name := disposition(p)
	return p.Type == "text" && (p.SubType == "plain" || p.SubType == "html") && disp != "attachment" && name == ""
}

func contentID(p *message.Part) string {
	return strings.Trim(strings.TrimSpace(p.Header.Get("Content-Id")), "<>")
}

// decode is a text part's body as UTF-8.
func decode(p *message.Part) string {
	b := p.DecodedBody()
	switch strings.ToLower(p.Params["charset"]) {
	case "iso-8859-1", "latin1", "windows-1252", "cp1252":
		rs := make([]rune, len(b))
		for i, c := range b {
			rs[i] = rune(c)
		}
		return string(rs)
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return strings.ToValidUTF8(string(b), "�")
}

// textOf turns HTML into readable text, for snippets, rules and replies.
func textOf(h string) string {
	h = blockRe.ReplaceAllString(h, "")
	h = breakRe.ReplaceAllString(h, "\n$0")
	h = tagRe.ReplaceAllString(h, "")
	h = html.UnescapeString(h)
	return tidy(h)
}

func tidy(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = spaceRe.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(linesRe.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

func addrs(h message.Header, name string) []Addr {
	v := h.Get(name)
	if v == "" {
		return nil
	}
	list, err := mail.ParseAddressList(v)
	if err != nil {
		// Unreadable, but still worth showing.
		return []Addr{{Name: clip(message.DecodeHeader(v), 200)}}
	}
	out := make([]Addr, 0, len(list))
	for _, a := range list {
		out = append(out, Addr{Name: a.Name, Email: strings.ToLower(a.Address)})
	}
	return out
}

func parse(raw []byte) *parsed {
	root := message.Parse(raw)
	h := root.Header
	p := &parsed{
		To:         addrs(h, "To"),
		Cc:         addrs(h, "Cc"),
		Bcc:        addrs(h, "Bcc"),
		ReplyTo:    addrs(h, "Reply-To"),
		Subject:    clip(message.DecodeHeader(h.Get("Subject")), 998),
		MessageID:  clip(h.Get("Message-Id"), 998),
		InReplyTo:  clip(h.Get("In-Reply-To"), 998),
		References: clip(h.Get("References"), 4000),
		Marketing:  message.Bulk(h),
	}
	if from := addrs(h, "From"); len(from) > 0 {
		p.From = from[0]
	}
	if t, err := mail.ParseDate(h.Get("Date")); err == nil {
		p.Sent = t
	}
	var text, htm string
	for i, part := range leaves(root) {
		if isBody(part) {
			switch {
			case part.SubType == "plain" && text == "":
				text = decode(part)
				continue
			case part.SubType == "html" && htm == "":
				htm = decode(part)
				continue
			}
		}
		disp, name := disposition(part)
		inline := part.Type == "image" && contentID(part) != "" && disp != "attachment"
		if name == "" {
			name = defaultName(part)
		}
		p.Attachments = append(p.Attachments, Attachment{Index: i, Name: clip(name, 255), Type: part.Type + "/" + part.SubType,
			Size: len(part.DecodedBody()), Inline: inline})
	}
	p.HasHTML = htm != ""
	p.Remote = remoteRe.MatchString(htm)
	switch {
	case text != "":
		p.Text = tidy(text)
	case htm != "":
		p.Text = textOf(htm)
	}
	p.Text = cut(p.Text, 64<<10)
	p.Snippet = cut(strings.Join(strings.Fields(p.Text), " "), 200)
	return p
}

func defaultName(p *message.Part) string {
	switch {
	case p.Type == "message" && p.SubType == "rfc822":
		return "message.eml"
	case p.Type == "text" && p.SubType == "calendar":
		return "invite.ics"
	case p.Type == "image":
		return "image." + p.SubType
	}
	return "attachment"
}

// htmlBody is the message's HTML with the pictures it carries put in place.
func htmlBody(raw []byte) string {
	root := message.Parse(raw)
	var htm string
	images := map[string]*message.Part{}
	for _, p := range leaves(root) {
		if htm == "" && isBody(p) && p.SubType == "html" {
			htm = decode(p)
		}
		if id := contentID(p); id != "" && p.Type == "image" {
			images[strings.ToLower(id)] = p
		}
	}
	if htm == "" || len(images) == 0 {
		return htm
	}
	budget := 20 << 20
	return cidRe.ReplaceAllStringFunc(htm, func(ref string) string {
		p := images[strings.ToLower(strings.TrimPrefix(strings.ToLower(ref), "cid:"))]
		if p == nil || budget <= 0 {
			return ref
		}
		b := p.DecodedBody()
		budget -= len(b)
		return dataURI(p.Type+"/"+p.SubType, b)
	})
}

var cidRe = regexp.MustCompile(`(?i)cid:[^"'\s)>]+`)

// cache keeps what's been parsed, by account, folder and UID; stored
// messages never change, so nothing goes stale.
type cache struct {
	mu sync.Mutex
	m  map[string]*parsed
}

func (c *cache) get(key string, read func() ([]byte, error)) *parsed {
	c.mu.Lock()
	p, ok := c.m[key]
	c.mu.Unlock()
	if ok {
		return p
	}
	raw, err := read()
	if err != nil {
		return &parsed{}
	}
	p = parse(raw)
	c.mu.Lock()
	if len(c.m) > 100_000 {
		c.m = map[string]*parsed{}
	}
	c.m[key] = p
	c.mu.Unlock()
	return p
}
