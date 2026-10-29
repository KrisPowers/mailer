package users

import "testing"

func TestAccountLimit(t *testing.T) {
	l := NewLimiter()
	l.Succeeded("a@example.com", "192.0.2.1")
	for i := 0; i < maxAccountFailures; i++ {
		if l.AccountBlocked("a@example.com", "198.51.100.1") {
			t.Fatalf("blocked after %d failures", i)
		}
		l.AccountFail("A@example.com")
	}
	if !l.AccountBlocked("a@example.com", "198.51.100.1") {
		t.Error("a new address can still try the account after the limit")
	}
	if l.AccountBlocked("a@example.com", "192.0.2.1") {
		t.Error("an address that signed in before is blocked")
	}
	if l.AccountBlocked("b@example.com", "198.51.100.1") {
		t.Error("another account is blocked")
	}
}
