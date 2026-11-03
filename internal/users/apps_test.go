package users

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMailApps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	h, _ := hash("correct horse battery")
	// An account saved before the webmail keeps its mail apps.
	os.WriteFile(path, []byte(`[{"email":"old@example.com","hash":"`+h+`"}]`), 0o600)
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := db.AuthenticateApp("old@example.com", "correct horse battery"); !ok {
		t.Error("an existing account lost its mail apps")
	}

	db.Add("new@example.com", "correct horse battery")
	if _, ok := db.Authenticate("new@example.com", "correct horse battery"); !ok {
		t.Error("a new account can't sign in to the webmail")
	}
	if _, ok := db.AuthenticateApp("new@example.com", "correct horse battery"); ok || !db.AppsOff("new@example.com") {
		t.Error("a new account's mail apps are on")
	}
	db.SetMailApps("new@example.com", true)
	again, _ := Open(path)
	if _, ok := again.AuthenticateApp("new@example.com", "correct horse battery"); !ok {
		t.Error("turning mail apps on didn't stick")
	}
	if again.AppsOff("ghost@example.com") {
		t.Error("a missing account reports its mail apps off")
	}
}
