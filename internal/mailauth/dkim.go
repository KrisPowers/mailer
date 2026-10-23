package mailauth

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"mailer/internal/message"
)

type DKIMStatus string

const (
	DKIMPass      DKIMStatus = "pass"
	DKIMFail      DKIMStatus = "fail"
	DKIMNeutral   DKIMStatus = "neutral"
	DKIMTempError DKIMStatus = "temperror"
	DKIMPermError DKIMStatus = "permerror"
)

type DKIMResult struct {
	Status   DKIMStatus
	Domain   string
	Selector string
	Reason   string
}

const maxSignatures = 5

// VerifyDKIM checks every DKIM-Signature header (up to a limit) on raw.
func VerifyDKIM(ctx context.Context, r Resolver, raw []byte) []DKIMResult {
	hdrBytes, body := message.SplitHeader(raw)
	header := message.ParseHeader(hdrBytes)
	var results []DKIMResult
	for i := range header {
		if !strings.EqualFold(header[i].Name, "DKIM-Signature") {
			continue
		}
		if len(results) >= maxSignatures {
			break
		}
		results = append(results, verifyOne(ctx, r, header, header[i], body))
	}
	return results
}

func parseTags(s string) (map[string]string, error) {
	tags := map[string]string{}
	for _, part := range strings.Split(s, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return nil, errSyntax
		}
		k = strings.TrimSpace(k)
		if _, dup := tags[k]; dup {
			return nil, errSyntax
		}
		tags[k] = strings.TrimSpace(v)
	}
	return tags, nil
}

func stripWS(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, s)
}

func verifyOne(ctx context.Context, r Resolver, header message.Header, sigField message.Field, body []byte) DKIMResult {
	res := DKIMResult{Status: DKIMPermError}
	tags, err := parseTags(sigField.Value)
	if err != nil {
		res.Reason = "malformed signature"
		return res
	}
	res.Domain = strings.ToLower(tags["d"])
	res.Selector = tags["s"]
	for _, k := range []string{"v", "a", "b", "bh", "d", "h", "s"} {
		if tags[k] == "" {
			res.Reason = "missing tag " + k
			return res
		}
	}
	if tags["v"] != "1" {
		res.Reason = "unsupported version"
		return res
	}
	algo := strings.ToLower(tags["a"])
	if algo != "rsa-sha256" && algo != "ed25519-sha256" {
		res.Reason = "unsupported algorithm " + algo
		return res
	}
	hcanon, bcanon := "simple", "simple"
	if c := strings.ToLower(tags["c"]); c != "" {
		h, b, hasB := strings.Cut(c, "/")
		hcanon = h
		if hasB {
			bcanon = b
		}
	}
	if (hcanon != "simple" && hcanon != "relaxed") || (bcanon != "simple" && bcanon != "relaxed") {
		res.Reason = "unknown canonicalization"
		return res
	}
	signedHeaders := strings.Split(stripWS(tags["h"]), ":")
	hasFrom := false
	for _, h := range signedHeaders {
		if strings.EqualFold(h, "from") {
			hasFrom = true
		}
	}
	if !hasFrom {
		res.Reason = "From not signed"
		return res
	}
	if auid := tags["i"]; auid != "" {
		at := strings.LastIndexByte(auid, '@')
		d := strings.ToLower(auid[at+1:])
		if at < 0 || (d != res.Domain && !strings.HasSuffix(d, "."+res.Domain)) {
			res.Reason = "i= not within d="
			return res
		}
	}
	if x := tags["x"]; x != "" {
		exp, err := strconv.ParseInt(x, 10, 64)
		if err == nil && time.Now().Unix() > exp {
			res.Reason = "signature expired"
			return res
		}
	}
	sig, err := base64.StdEncoding.DecodeString(stripWS(tags["b"]))
	if err != nil {
		res.Reason = "bad b= encoding"
		return res
	}
	bodyHash, err := base64.StdEncoding.DecodeString(stripWS(tags["bh"]))
	if err != nil {
		res.Reason = "bad bh= encoding"
		return res
	}

	// Body hash.
	cbody := canonBody(body, bcanon)
	if l := tags["l"]; l != "" {
		n, err := strconv.ParseInt(l, 10, 64)
		if err != nil || n < 0 || n > int64(len(cbody)) {
			res.Reason = "invalid l="
			return res
		}
		cbody = cbody[:n]
	}
	if sum := sha256.Sum256(cbody); !bytes.Equal(sum[:], bodyHash) {
		res.Status, res.Reason = DKIMFail, "body hash mismatch"
		return res
	}

	// Public key.
	pub, kerr := fetchKey(ctx, r, res.Selector, res.Domain, algo)
	if kerr != nil {
		res.Status, res.Reason = kerr.status, kerr.reason
		return res
	}

	// Header hash: signed headers bottom-up, then the signature header
	// with an empty b= value and no trailing CRLF.
	h := sha256.New()
	used := map[int]bool{}
	for _, name := range signedHeaders {
		for i := len(header) - 1; i >= 0; i-- {
			if used[i] || !strings.EqualFold(header[i].Name, name) {
				continue
			}
			used[i] = true
			h.Write(canonHeader(header[i].Raw, hcanon))
			break
		}
	}
	h.Write(bytes.TrimSuffix(canonHeader(removeBValue(sigField.Raw), hcanon), []byte("\r\n")))
	digest := h.Sum(nil)

	switch k := pub.(type) {
	case *rsa.PublicKey:
		if k.N.BitLen() < 1024 {
			res.Reason = "RSA key too short"
			return res
		}
		err = rsa.VerifyPKCS1v15(k, crypto.SHA256, digest, sig)
	case ed25519.PublicKey:
		if !ed25519.Verify(k, digest, sig) {
			err = errors.New("bad signature")
		}
	}
	if err != nil {
		res.Status, res.Reason = DKIMFail, "signature did not verify"
		return res
	}
	res.Status, res.Reason = DKIMPass, ""
	return res
}

type keyError struct {
	status DKIMStatus
	reason string
}

func fetchKey(ctx context.Context, r Resolver, selector, domain, algo string) (crypto.PublicKey, *keyError) {
	txts, err := r.LookupTXT(ctx, selector+"._domainkey."+domain)
	if err != nil {
		if isNotFound(err) {
			return nil, &keyError{DKIMPermError, "no key record"}
		}
		return nil, &keyError{DKIMTempError, "key lookup failed"}
	}
	if len(txts) == 0 {
		return nil, &keyError{DKIMPermError, "no key record"}
	}
	tags, err := parseTags(strings.Join(txts, ""))
	if err != nil {
		tags, err = parseTags(txts[0])
	}
	if err != nil {
		return nil, &keyError{DKIMPermError, "malformed key record"}
	}
	if v, ok := tags["v"]; ok && v != "DKIM1" {
		return nil, &keyError{DKIMPermError, "bad key version"}
	}
	p := stripWS(tags["p"])
	if p == "" {
		return nil, &keyError{DKIMPermError, "key revoked"}
	}
	der, err := base64.StdEncoding.DecodeString(p)
	if err != nil {
		return nil, &keyError{DKIMPermError, "bad key encoding"}
	}
	kt := strings.ToLower(tags["k"])
	if kt == "" {
		kt = "rsa"
	}
	switch {
	case kt == "rsa" && algo == "rsa-sha256":
		if pub, err := x509.ParsePKIXPublicKey(der); err == nil {
			if rk, ok := pub.(*rsa.PublicKey); ok {
				return rk, nil
			}
		}
		if rk, err := x509.ParsePKCS1PublicKey(der); err == nil {
			return rk, nil
		}
		return nil, &keyError{DKIMPermError, "unparseable RSA key"}
	case kt == "ed25519" && algo == "ed25519-sha256":
		if len(der) != ed25519.PublicKeySize {
			return nil, &keyError{DKIMPermError, "bad ed25519 key"}
		}
		return ed25519.PublicKey(der), nil
	}
	return nil, &keyError{DKIMPermError, "key type does not match algorithm"}
}

var bTagRe = regexp.MustCompile(`(^|;)(\s*b\s*=)[^;]*`)

func removeBValue(raw []byte) []byte {
	name, value, _ := bytes.Cut(raw, []byte(":"))
	value = bTagRe.ReplaceAll(value, []byte("$1$2"))
	out := append(append([]byte{}, name...), ':')
	return append(out, value...)
}

var wspRun = regexp.MustCompile(`[ \t]+`)

func canonHeader(raw []byte, canon string) []byte {
	if canon == "simple" {
		return raw
	}
	name, value, _ := bytes.Cut(raw, []byte(":"))
	n := strings.ToLower(strings.TrimRight(string(name), " \t"))
	v := strings.ReplaceAll(string(value), "\r\n", "")
	v = wspRun.ReplaceAllString(v, " ")
	v = strings.TrimSpace(v)
	return []byte(n + ":" + v + "\r\n")
}

func canonBody(body []byte, canon string) []byte {
	if canon == "relaxed" {
		lines := bytes.Split(body, []byte("\r\n"))
		for i, l := range lines {
			l = wspRun.ReplaceAll(l, []byte(" "))
			lines[i] = bytes.TrimRight(l, " ")
		}
		body = bytes.Join(lines, []byte("\r\n"))
	}
	// Remove trailing empty lines, then ensure exactly one CRLF.
	for bytes.HasSuffix(body, []byte("\r\n")) {
		body = body[:len(body)-2]
	}
	if len(body) == 0 {
		if canon == "relaxed" {
			return []byte{}
		}
		return []byte("\r\n")
	}
	return append(append([]byte{}, body...), '\r', '\n')
}

// ---- signing ----

// Signer produces DKIM signatures for one domain.
type Signer struct {
	Domain   string
	Selector string
	Key      *rsa.PrivateKey
}

var signedHeaderNames = []string{
	"From", "Reply-To", "Subject", "Date", "To", "Cc", "Message-ID",
	"In-Reply-To", "References", "MIME-Version", "Content-Type",
	"Content-Transfer-Encoding",
}

// Sign returns a complete "DKIM-Signature: ...\r\n" header to prepend to raw.
func (s *Signer) Sign(raw []byte) ([]byte, error) {
	hdrBytes, body := message.SplitHeader(raw)
	header := message.ParseHeader(hdrBytes)

	var names []string
	for _, n := range signedHeaderNames {
		if header.Has(n) {
			names = append(names, n)
		}
	}
	// Sign From an extra time so a second From can't be added later.
	names = append(names, "From")

	bh := sha256.Sum256(canonBody(body, "relaxed"))
	value := fmt.Sprintf(" v=1; a=rsa-sha256; c=relaxed/relaxed; d=%s; s=%s;\r\n\tt=%d; h=%s;\r\n\tbh=%s;\r\n\tb=",
		s.Domain, s.Selector, time.Now().Unix(), strings.Join(names, ":"),
		base64.StdEncoding.EncodeToString(bh[:]))
	sigHeader := "DKIM-Signature:" + value

	h := sha256.New()
	used := map[int]bool{}
	for _, name := range names {
		for i := len(header) - 1; i >= 0; i-- {
			if used[i] || !strings.EqualFold(header[i].Name, name) {
				continue
			}
			used[i] = true
			h.Write(canonHeader(header[i].Raw, "relaxed"))
			break
		}
	}
	h.Write(bytes.TrimSuffix(canonHeader([]byte(sigHeader), "relaxed"), []byte("\r\n")))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.Key, crypto.SHA256, h.Sum(nil))
	if err != nil {
		return nil, err
	}
	b64 := base64.StdEncoding.EncodeToString(sig)
	var out strings.Builder
	out.WriteString(sigHeader)
	for len(b64) > 72 {
		out.WriteString(b64[:72])
		out.WriteString("\r\n\t")
		b64 = b64[72:]
	}
	out.WriteString(b64)
	out.WriteString("\r\n")
	return []byte(out.String()), nil
}

// DNSRecord returns the TXT record value publishing the signer's public key.
func (s *Signer) DNSRecord() string {
	der, _ := x509.MarshalPKIXPublicKey(&s.Key.PublicKey)
	return "v=DKIM1; k=rsa; p=" + base64.StdEncoding.EncodeToString(der)
}
