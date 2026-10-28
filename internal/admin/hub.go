package admin

import (
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"mailer/internal/auth"
)

// A hub is a dashboard that runs mailer as one of several apps, such as
// truebrew. It signs people in itself and calls this API on their behalf
// with a key it shares with mailer through a file, naming the person and
// their role in headers. Mailer's own sign-in, device approval and setup
// are then the hub's, so its own first-run setup is skipped.
const (
	hubUserHeader = "X-Hub-User"
	hubRoleHeader = "X-Hub-Role"
)

// managed reports whether a hub key file is configured.
func (s *Server) managed() bool { return s.HubKeyFile != "" }

// fromHub returns the person a hub request is made for, if r carries the
// hub's key. The key file is read on each request, so it may appear after
// mailer starts.
func (s *Server) fromHub(r *http.Request) (auth.Claims, bool) {
	if !s.managed() {
		return auth.Claims{}, false
	}
	b, err := os.ReadFile(s.HubKeyFile)
	key := strings.TrimSpace(string(b))
	given, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if err != nil || key == "" || !ok || subtle.ConstantTimeCompare([]byte(key), []byte(given)) != 1 {
		return auth.Claims{}, false
	}
	role := auth.RoleUser
	if r.Header.Get(hubRoleHeader) == auth.RoleAdmin {
		role = auth.RoleAdmin
	}
	return auth.Claims{Subject: r.Header.Get(hubUserHeader), Role: role}, true
}
