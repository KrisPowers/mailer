package mailauth

import (
	"context"
	"strings"
)

type DMARCResult struct {
	Result string // pass, fail, none, temperror, permerror
	Policy string // none, quarantine, reject (the policy that applies)
	Domain string // From: domain
}

// CheckDMARC evaluates DMARC for the RFC5322.From domain given the SPF
// result (for spfDomain, the MAIL FROM domain) and DKIM results.
func CheckDMARC(ctx context.Context, r Resolver, fromDomain string, spf SPFResult, spfDomain string, dkim []DKIMResult) DMARCResult {
	fromDomain = strings.ToLower(strings.TrimSuffix(fromDomain, "."))
	res := DMARCResult{Result: "none", Policy: "none", Domain: fromDomain}
	if !validDomain(fromDomain) {
		return res
	}
	org := OrgDomain(fromDomain)

	tags, found, temp := lookupDMARC(ctx, r, fromDomain)
	atOrg := false
	if !found && !temp && org != fromDomain {
		tags, found, temp = lookupDMARC(ctx, r, org)
		atOrg = true
	}
	if temp {
		res.Result = "temperror"
		return res
	}
	if !found {
		return res
	}

	p := strings.ToLower(tags["p"])
	if p != "none" && p != "quarantine" && p != "reject" {
		if tags["rua"] == "" {
			res.Result = "permerror"
			return res
		}
		p = "none"
	}
	if sp := strings.ToLower(tags["sp"]); atOrg && sp != "" {
		p = sp
	}
	res.Policy = p

	aligned := func(d, mode string) bool {
		d = strings.ToLower(d)
		if strings.ToLower(mode) == "s" {
			return d == fromDomain
		}
		return OrgDomain(d) == org
	}
	if spf == SPFPass && aligned(spfDomain, tags["aspf"]) {
		res.Result = "pass"
		return res
	}
	for _, d := range dkim {
		if d.Status == DKIMPass && aligned(d.Domain, tags["adkim"]) {
			res.Result = "pass"
			return res
		}
	}
	res.Result = "fail"
	return res
}

func lookupDMARC(ctx context.Context, r Resolver, domain string) (tags map[string]string, found, temp bool) {
	txts, err := r.LookupTXT(ctx, "_dmarc."+domain)
	if err != nil {
		return nil, false, !isNotFound(err)
	}
	var records []string
	for _, t := range txts {
		if strings.HasPrefix(strings.TrimSpace(t), "v=DMARC1") {
			records = append(records, t)
		}
	}
	if len(records) != 1 {
		return nil, false, false
	}
	tags, err = parseTags(records[0])
	if err != nil {
		return nil, false, false
	}
	return tags, true, false
}

// multiLabelSuffixes lists common public suffixes with more than one label.
// It is a deliberately small subset of the Public Suffix List; domains under
// unlisted multi-label suffixes are treated as if the suffix were one label.
var multiLabelSuffixes = map[string]bool{
	"co.uk": true, "org.uk": true, "ac.uk": true, "gov.uk": true, "me.uk": true, "ltd.uk": true, "plc.uk": true,
	"com.au": true, "net.au": true, "org.au": true, "edu.au": true, "gov.au": true,
	"co.nz": true, "org.nz": true, "net.nz": true,
	"co.jp": true, "ne.jp": true, "or.jp": true, "ac.jp": true,
	"com.br": true, "net.br": true, "org.br": true,
	"com.mx": true, "com.ar": true, "com.cn": true, "com.hk": true, "com.sg": true, "com.tw": true,
	"co.in": true, "net.in": true, "org.in": true,
	"co.za": true, "co.kr": true, "co.il": true, "com.tr": true, "com.ua": true,
}

// OrgDomain approximates the organizational domain (registrable domain).
func OrgDomain(domain string) string {
	labels := strings.Split(strings.ToLower(strings.TrimSuffix(domain, ".")), ".")
	if len(labels) <= 2 {
		return strings.Join(labels, ".")
	}
	last2 := strings.Join(labels[len(labels)-2:], ".")
	if multiLabelSuffixes[last2] {
		return strings.Join(labels[len(labels)-3:], ".")
	}
	return last2
}
