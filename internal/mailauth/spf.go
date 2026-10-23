// Package mailauth implements SPF (RFC 7208), DKIM (RFC 6376, 8463) and
// DMARC (RFC 7489) evaluation, plus DKIM signing.
package mailauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Resolver is the subset of *net.Resolver used here; tests substitute a fake.
type Resolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

func isNotFound(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de) && de.IsNotFound
}

type SPFResult string

const (
	SPFNone      SPFResult = "none"
	SPFNeutral   SPFResult = "neutral"
	SPFPass      SPFResult = "pass"
	SPFFail      SPFResult = "fail"
	SPFSoftFail  SPFResult = "softfail"
	SPFTempError SPFResult = "temperror"
	SPFPermError SPFResult = "permerror"
)

// CheckSPF evaluates the SPF policy for a connection. sender is the MAIL
// FROM address (empty for bounces, in which case postmaster@helo is used).
// It returns the result and the domain that was checked.
func CheckSPF(ctx context.Context, r Resolver, ip net.IP, sender, helo string) (SPFResult, string) {
	if sender == "" || !strings.Contains(sender, "@") {
		sender = "postmaster@" + helo
	}
	at := strings.LastIndexByte(sender, '@')
	domain := strings.ToLower(strings.TrimSuffix(sender[at+1:], "."))
	c := &spfChecker{r: r, ip: ip, sender: sender, helo: helo}
	return c.checkHost(ctx, domain, 0), domain
}

type spfChecker struct {
	r            Resolver
	ip           net.IP
	sender, helo string
	lookups      int
	voids        int
}

var errLimit = errors.New("spf lookup limit exceeded")

func (c *spfChecker) countLookup() error {
	c.lookups++
	if c.lookups > 10 {
		return errLimit
	}
	return nil
}

func (c *spfChecker) void(n int, err error) error {
	if err == nil && n > 0 {
		return nil
	}
	if err != nil && !isNotFound(err) {
		return err
	}
	c.voids++
	if c.voids > 2 {
		return errLimit
	}
	return nil
}

func validDomain(d string) bool {
	if d == "" || len(d) > 253 {
		return false
	}
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 {
			return false
		}
	}
	return true
}

func (c *spfChecker) checkHost(ctx context.Context, domain string, depth int) SPFResult {
	if depth > 10 {
		return SPFPermError
	}
	if !validDomain(domain) {
		return SPFNone
	}
	txts, err := c.r.LookupTXT(ctx, domain)
	if err != nil {
		if isNotFound(err) {
			return SPFNone
		}
		return SPFTempError
	}
	var record string
	for _, t := range txts {
		lt := strings.ToLower(t)
		if lt == "v=spf1" || strings.HasPrefix(lt, "v=spf1 ") {
			if record != "" {
				return SPFPermError
			}
			record = t
		}
	}
	if record == "" {
		return SPFNone
	}

	var redirect string
	terms := strings.Fields(record)[1:]
	for _, term := range terms {
		// Modifier: name=value where name precedes any ':' or '/'.
		if eq := strings.IndexByte(term, '='); eq > 0 && !strings.ContainsAny(term[:eq], ":/") {
			name := strings.ToLower(term[:eq])
			if name == "redirect" {
				redirect = term[eq+1:]
			}
			continue
		}
		qual := SPFPass
		switch term[0] {
		case '+':
			term = term[1:]
		case '-':
			qual, term = SPFFail, term[1:]
		case '~':
			qual, term = SPFSoftFail, term[1:]
		case '?':
			qual, term = SPFNeutral, term[1:]
		}
		match, res := c.mechanism(ctx, domain, term, depth)
		if res != "" {
			return res
		}
		if match {
			return qual
		}
	}
	if redirect != "" {
		if err := c.countLookup(); err != nil {
			return SPFPermError
		}
		target, err := c.expand(redirect, domain)
		if err != nil {
			return SPFPermError
		}
		res := c.checkHost(ctx, target, depth+1)
		if res == SPFNone {
			return SPFPermError
		}
		return res
	}
	return SPFNeutral
}

// mechanism evaluates one mechanism. A non-empty result aborts evaluation.
func (c *spfChecker) mechanism(ctx context.Context, domain, term string, depth int) (bool, SPFResult) {
	name, arg := term, ""
	if i := strings.IndexAny(term, ":/"); i >= 0 {
		name, arg = term[:i], term[i:]
	}
	name = strings.ToLower(name)
	errResult := func(err error) (bool, SPFResult) {
		if errors.Is(err, errLimit) {
			return false, SPFPermError
		}
		return false, SPFTempError
	}

	switch name {
	case "all":
		return true, ""

	case "include":
		if err := c.countLookup(); err != nil {
			return false, SPFPermError
		}
		target, err := c.expand(strings.TrimPrefix(arg, ":"), domain)
		if err != nil || target == "" {
			return false, SPFPermError
		}
		switch c.checkHost(ctx, target, depth+1) {
		case SPFPass:
			return true, ""
		case SPFTempError:
			return false, SPFTempError
		case SPFPermError, SPFNone:
			return false, SPFPermError
		}
		return false, ""

	case "a", "mx":
		if err := c.countLookup(); err != nil {
			return false, SPFPermError
		}
		target, cidr4, cidr6, err := c.domainAndCIDR(arg, domain)
		if err != nil {
			return false, SPFPermError
		}
		var hosts []string
		if name == "mx" {
			mxs, err := c.r.LookupMX(ctx, target)
			if err := c.void(len(mxs), err); err != nil {
				return errResult(err)
			}
			for i, mx := range mxs {
				if i >= 10 {
					return false, SPFPermError
				}
				hosts = append(hosts, mx.Host)
			}
		} else {
			hosts = []string{target}
		}
		for _, h := range hosts {
			addrs, err := c.r.LookupIPAddr(ctx, h)
			if name == "a" {
				if err := c.void(len(addrs), err); err != nil {
					return errResult(err)
				}
			} else if err != nil && !isNotFound(err) {
				return false, SPFTempError
			}
			for _, a := range addrs {
				if c.ipMatch(a.IP, cidr4, cidr6) {
					return true, ""
				}
			}
		}
		return false, ""

	case "ptr":
		if err := c.countLookup(); err != nil {
			return false, SPFPermError
		}
		target := domain
		if strings.HasPrefix(arg, ":") {
			t, err := c.expand(arg[1:], domain)
			if err != nil {
				return false, SPFPermError
			}
			target = t
		}
		names, _ := c.r.LookupAddr(ctx, c.ip.String())
		for i, n := range names {
			if i >= 10 {
				break
			}
			n = strings.ToLower(strings.TrimSuffix(n, "."))
			if n != target && !strings.HasSuffix(n, "."+target) {
				continue
			}
			addrs, _ := c.r.LookupIPAddr(ctx, n)
			for _, a := range addrs {
				if a.IP.Equal(c.ip) {
					return true, ""
				}
			}
		}
		return false, ""

	case "ip4", "ip6":
		spec := strings.TrimPrefix(arg, ":")
		if !strings.Contains(spec, "/") {
			if name == "ip4" {
				spec += "/32"
			} else {
				spec += "/128"
			}
		}
		_, n, err := net.ParseCIDR(spec)
		if err != nil {
			return false, SPFPermError
		}
		if (name == "ip4") != (n.IP.To4() != nil) {
			return false, SPFPermError
		}
		return n.Contains(c.ip), ""

	case "exists":
		if err := c.countLookup(); err != nil {
			return false, SPFPermError
		}
		target, err := c.expand(strings.TrimPrefix(arg, ":"), domain)
		if err != nil {
			return false, SPFPermError
		}
		addrs, err := c.r.LookupIPAddr(ctx, target)
		if err := c.void(len(addrs), err); err != nil {
			return errResult(err)
		}
		for _, a := range addrs {
			if a.IP.To4() != nil {
				return true, ""
			}
		}
		return false, ""
	}
	return false, SPFPermError
}

// domainAndCIDR parses "[:domain][/cidr4][//cidr6]".
func (c *spfChecker) domainAndCIDR(arg, domain string) (string, int, int, error) {
	cidr4, cidr6 := 32, 128
	if i := strings.Index(arg, "//"); i >= 0 {
		n, err := strconv.Atoi(arg[i+2:])
		if err != nil || n < 0 || n > 128 {
			return "", 0, 0, errSyntax
		}
		cidr6, arg = n, arg[:i]
	}
	if i := strings.LastIndexByte(arg, '/'); i >= 0 {
		n, err := strconv.Atoi(arg[i+1:])
		if err != nil || n < 0 || n > 32 {
			return "", 0, 0, errSyntax
		}
		cidr4, arg = n, arg[:i]
	}
	target := domain
	if strings.HasPrefix(arg, ":") {
		t, err := c.expand(arg[1:], domain)
		if err != nil {
			return "", 0, 0, err
		}
		target = t
	} else if arg != "" {
		return "", 0, 0, errSyntax
	}
	return target, cidr4, cidr6, nil
}

func (c *spfChecker) ipMatch(addr net.IP, cidr4, cidr6 int) bool {
	if v4 := c.ip.To4(); v4 != nil {
		a4 := addr.To4()
		if a4 == nil {
			return false
		}
		m := net.CIDRMask(cidr4, 32)
		return v4.Mask(m).Equal(a4.Mask(m))
	}
	if addr.To4() != nil {
		return false
	}
	m := net.CIDRMask(cidr6, 128)
	return c.ip.Mask(m).Equal(addr.To16().Mask(m))
}

var errSyntax = errors.New("syntax error")

// expand performs SPF macro expansion (RFC 7208 section 7).
func (c *spfChecker) expand(s, domain string) (string, error) {
	if !strings.Contains(s, "%") {
		return strings.ToLower(strings.TrimSuffix(s, ".")), nil
	}
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			out.WriteByte(s[i])
			continue
		}
		i++
		if i >= len(s) {
			return "", errSyntax
		}
		switch s[i] {
		case '%':
			out.WriteByte('%')
			continue
		case '_':
			out.WriteByte(' ')
			continue
		case '-':
			out.WriteString("%20")
			continue
		case '{':
		default:
			return "", errSyntax
		}
		end := strings.IndexByte(s[i:], '}')
		if end < 2 {
			return "", errSyntax
		}
		spec := s[i+1 : i+end]
		i += end
		letter := spec[0]
		rest := spec[1:]
		digits := 0
		for len(rest) > 0 && rest[0] >= '0' && rest[0] <= '9' {
			digits = digits*10 + int(rest[0]-'0')
			rest = rest[1:]
		}
		reverse := false
		if len(rest) > 0 && (rest[0] == 'r' || rest[0] == 'R') {
			reverse, rest = true, rest[1:]
		}
		delims := rest
		if delims == "" {
			delims = "."
		}

		local, senderDomain, _ := strings.Cut(c.sender, "@")
		var val string
		switch letter | 0x20 {
		case 's':
			val = c.sender
		case 'l':
			val = local
		case 'o':
			val = senderDomain
		case 'd':
			val = domain
		case 'h':
			val = c.helo
		case 'v':
			if c.ip.To4() != nil {
				val = "in-addr"
			} else {
				val = "ip6"
			}
		case 'p':
			val = "unknown"
		case 'i':
			if v4 := c.ip.To4(); v4 != nil {
				val = v4.String()
			} else {
				hexs := fmt.Sprintf("%x", []byte(c.ip.To16()))
				val = strings.Join(strings.Split(hexs, ""), ".")
			}
		default:
			return "", errSyntax
		}

		parts := strings.FieldsFunc(val, func(r rune) bool { return strings.ContainsRune(delims, r) })
		if reverse {
			for a, b := 0, len(parts)-1; a < b; a, b = a+1, b-1 {
				parts[a], parts[b] = parts[b], parts[a]
			}
		}
		if digits > 0 && digits < len(parts) {
			parts = parts[len(parts)-digits:]
		}
		val = strings.Join(parts, ".")
		if letter >= 'A' && letter <= 'Z' {
			val = url.QueryEscape(val)
		}
		out.WriteString(val)
	}
	res := strings.ToLower(strings.TrimSuffix(out.String(), "."))
	for len(res) > 253 { // truncate from the left per RFC 7208 7.3
		_, after, ok := strings.Cut(res, ".")
		if !ok {
			break
		}
		res = after
	}
	return res, nil
}
