package webmail

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"sync"
	"time"

	"mailer/internal/users"
)

const (
	// A session ends 30 days after it started, or after two weeks unused.
	sessionLife = 30 * 24 * time.Hour
	sessionIdle = 14 * 24 * time.Hour
	// Last use is written to disk at most this often.
	touchEvery = 5 * time.Minute
)

type session struct {
	Email string `json:"email"`
	// Stamp is the account's password stamp when it signed in; a new
	// password ends the session.
	Stamp   string    `json:"stamp"`
	Created time.Time `json:"created"`
	Used    time.Time `json:"used"`
	IP      string    `json:"ip"`
	Agent   string    `json:"agent"`
}

// sessions are the webmail's sign-ins, kept in a file so a restart doesn't
// sign everyone out. Only a hash of each token is stored.
type sessions struct {
	path string
	mu   sync.Mutex
	m    map[string]*session
}

func openSessions(path string) *sessions {
	s := &sessions{path: path, m: map[string]*session{}}
	if b, err := os.ReadFile(path); err == nil {
		json.Unmarshal(b, &s.m)
	}
	if s.m == nil {
		s.m = map[string]*session{}
	}
	return s
}

func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *sessions) add(email, stamp, ip, agent string) (string, error) {
	b := make([]byte, 32)
	rand.Read(b)
	token := base64.RawURLEncoding.EncodeToString(b)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.prune(now)
	s.m[tokenKey(token)] = &session{Email: email, Stamp: stamp, Created: now, Used: now, IP: ip, Agent: agent}
	return token, s.save()
}

// get returns the session token opens, if it's still live, and notes its use.
func (s *sessions) get(token string) (session, bool) {
	if token == "" {
		return session{}, false
	}
	key := tokenKey(token)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	se, ok := s.m[key]
	if !ok {
		return session{}, false
	}
	if now.Sub(se.Created) > sessionLife || now.Sub(se.Used) > sessionIdle {
		delete(s.m, key)
		s.save()
		return session{}, false
	}
	if now.Sub(se.Used) > touchEvery {
		se.Used = now
		s.save()
	}
	return *se, true
}

func (s *sessions) end(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tokenKey(token)
	if _, ok := s.m[key]; ok {
		delete(s.m, key)
		s.save()
	}
}

func (s *sessions) prune(now time.Time) {
	for k, se := range s.m {
		if now.Sub(se.Created) > sessionLife || now.Sub(se.Used) > sessionIdle {
			delete(s.m, k)
		}
	}
}

func (s *sessions) save() error {
	b, err := json.Marshal(s.m)
	if err != nil {
		return err
	}
	return users.WriteFileAtomic(s.path, b, 0o600)
}
