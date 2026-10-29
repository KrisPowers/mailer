package imap

import "testing"

func TestLiteralLongerThanCommand(t *testing.T) {
	for _, s := range []string{"{9223372036854775807}\r\nx", "{5}\r\nabc"} {
		if _, err := parseArgs([]byte(s)); err == nil {
			t.Errorf("%q: accepted a literal longer than the command", s)
		}
	}
}

// FuzzParse feeds command arguments, which come from the internet before
// sign-in, through the parsers; none may panic.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"a \"b\" (c d) {1}\r\nx",
		"1:* (FLAGS BODY.PEEK[HEADER.FIELDS (FROM TO)]<0.100>)",
		"~{3}\r\nabc",
		"OR FROM a TO b NOT (SEEN UID 1:*)",
		"\"unterminated",
		"((()))",
		"{9223372036854775807}\r\nx",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		toks, err := parseArgs(b)
		if err != nil {
			return
		}
		for _, tok := range toks {
			parseFetchItems(tok)
			if tok.kind == tAtom {
				parseSeqSet(tok.s)
			}
		}
		parseSearch(toks)
	})
}
