package auth

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const pw = "correct horse battery"

func open(t *testing.T) *Store {
	s, err := Open(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// setup makes the first admin and returns its device ID and tokens.
func setup(t *testing.T, s *Store) (string, Tokens) {
	id, _, err := s.NewDevice("test", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := s.Setup("Kris", pw, id, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	return id, tok
}

func TestToken(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	now := time.Now()
	c := Claims{Issuer: issuer, Subject: "kris", Role: RoleAdmin, Session: "s", Device: "d", Issued: now.Unix(), Expires: now.Add(time.Minute).Unix()}
	tok := sign(key, c)
	if got, err := parse(key, tok, now); err != nil || got != c {
		t.Fatalf("round trip: %v %+v", err, got)
	}
	if _, err := parse([]byte("another key, just as long as it"), tok, now); err == nil {
		t.Error("accepted with the wrong key")
	}
	if _, err := parse(key, tok, now.Add(2*time.Minute)); err == nil {
		t.Error("accepted after expiry")
	}
	// Changing the claims breaks the signature.
	parts := strings.Split(tok, ".")
	c.Role = "root"
	forged := sign(key, c)
	if _, err := parse(key, parts[0]+"."+strings.Split(forged, ".")[1]+"."+parts[2], now); err == nil {
		t.Error("accepted altered claims")
	}
	// An unsigned token is refused whatever it claims.
	none := b64.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`)) + "." + parts[1] + "."
	if _, err := parse(key, none, now); err == nil {
		t.Error(`accepted "alg":"none"`)
	}
}

func TestSetupAndLogin(t *testing.T) {
	s := open(t)
	if !s.NeedsSetup() {
		t.Fatal("new store doesn't need setup")
	}
	id, tok := setup(t, s)
	if tok.User.Name != "kris" || tok.User.Role != RoleAdmin {
		t.Errorf("first account: %+v", tok.User)
	}
	if _, err := s.Setup("other", pw, id, "", ""); err != ErrSetupDone {
		t.Errorf("second setup: %v", err)
	}
	if c, err := s.Verify(tok.Access, id); err != nil || c.Subject != "kris" {
		t.Errorf("verify: %v %+v", err, c)
	}

	// A new device can't sign in until approved.
	other, d, _ := s.NewDevice("phone", "203.0.113.9")
	if _, err := s.Login("kris", pw, other, "", ""); err != ErrDevice {
		t.Errorf("unapproved device: %v", err)
	}
	if _, err := s.Login("kris", pw, "made-up", "", ""); err != ErrDevice {
		t.Errorf("unknown device: %v", err)
	}
	s.Approve(d.Key, "Phone", "kris")
	if _, err := s.Login("kris", "wrong password!", other, "", ""); err != ErrCredentials {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := s.Login("nobody", pw, other, "", ""); err != ErrCredentials {
		t.Errorf("unknown user: %v", err)
	}
	if _, err := s.Login("KRIS", pw, other, "", ""); err != nil {
		t.Errorf("approved device: %v", err)
	}

	// The store survives a restart.
	s2, err := Open(s.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s2.Verify(tok.Access, id); err != nil {
		t.Errorf("after reopening: %v", err)
	}
}

func TestRefresh(t *testing.T) {
	s := open(t)
	id, tok := setup(t, s)
	next, err := s.Refresh(tok.Refresh, id, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Refresh(next.Refresh, "another device", ""); err == nil {
		t.Error("refreshed from another device")
	}
	// A second tab refreshing with the same cookie a moment later gets a
	// token, but no new refresh token.
	if race, err := s.Refresh(tok.Refresh, id, ""); err != nil || race.Access == "" || race.Refresh != "" {
		t.Errorf("simultaneous refresh: %v %+v", err, race)
	}
	// Later, using a replaced refresh token again ends the session.
	s.now = func() time.Time { return time.Now().Add(time.Minute) }
	if _, err := s.Refresh(tok.Refresh, id, ""); err != ErrSession {
		t.Errorf("replayed refresh token: %v", err)
	}
	if _, err := s.Refresh(next.Refresh, id, ""); err != ErrSession {
		t.Errorf("session survived a replay: %v", err)
	}
	if _, err := s.Verify(next.Access, id); err != ErrSession {
		t.Errorf("token survived a replay: %v", err)
	}
}

func TestRevocation(t *testing.T) {
	s := open(t)
	id, tok := setup(t, s)
	u, err := s.AddUser("sam", pw, RoleUser, "kris")
	if err != nil || u.Role != RoleUser {
		t.Fatal(err)
	}
	st, err := s.Login("sam", pw, id, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetDisabled("sam", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(st.Access, id); err == nil {
		t.Error("disabled account still signed in")
	}
	if _, err := s.Login("sam", pw, id, "", ""); err != ErrCredentials {
		t.Errorf("disabled account signed in: %v", err)
	}

	// A password change ends other sessions but keeps the one making it.
	c, _ := s.Verify(tok.Access, id)
	second, _ := s.Login("kris", pw, id, "", "")
	if err := s.SetPassword("kris", "a brand new password", c.Session); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(tok.Access, id); err != nil {
		t.Errorf("own session ended: %v", err)
	}
	if _, err := s.Verify(second.Access, id); err == nil {
		t.Error("other session survived a password change")
	}

	// The last admin can't be removed, turned off or demoted.
	if s.DeleteUser("kris") == nil || s.SetDisabled("kris", true) == nil || s.SetRole("kris", RoleUser) == nil {
		t.Error("last admin removed")
	}
}

func TestPendingDevices(t *testing.T) {
	s := open(t)
	first, _, _ := s.NewDevice("a", "")
	for range maxPending + 5 {
		s.NewDevice("b", "")
	}
	if n := len(s.Devices()); n != maxPending {
		t.Errorf("%d devices waiting, want %d", n, maxPending)
	}
	if _, ok := s.Device(first); ok {
		t.Error("oldest waiting device kept")
	}
	// Waiting devices are forgotten after a week.
	s.now = func() time.Time { return time.Now().Add(pendingTTL + time.Hour) }
	if n := len(s.Devices()); n != 0 {
		t.Errorf("%d stale devices kept", n)
	}
}

func TestWindow(t *testing.T) {
	w := &Window{Max: 3, Per: time.Minute}
	for i := range 3 {
		if !w.Allow("a") {
			t.Fatalf("refused attempt %d", i+1)
		}
	}
	if w.Allow("a") || w.Until("a").IsZero() {
		t.Error("fourth attempt allowed")
	}
	if !w.Allow("b") {
		t.Error("other key limited")
	}
	w.Reset("a")
	if w.Full("a") {
		t.Error("reset didn't clear")
	}
}
