package store

import "testing"

func TestAccountName(t *testing.T) {
	for _, ok := range []string{"me@example.com", "Kris.P+tag@Mail.Example.com", "o'neil@example.com"} {
		if _, err := AccountName(ok); err != nil {
			t.Errorf("%q refused: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "../etc@example.com", "a/b@example.com", `a\b@example.com`, ".hidden@example.com",
		"me@..", "me@example..com", "no-at-sign", "me@example.com/../x"} {
		if _, err := AccountName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	s := New(t.TempDir())
	if _, err := s.Account("../outside@example.com"); err == nil {
		t.Error("an account outside the store opened")
	}
}
