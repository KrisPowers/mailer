package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Claims are what a dashboard token carries. Sid ties it to a session and
// Dev to the device it was issued to, so revoking either ends it at once.
type Claims struct {
	Issuer  string `json:"iss"`
	Subject string `json:"sub"`
	Role    string `json:"role"`
	Session string `json:"sid"`
	Device  string `json:"dev"`
	Issued  int64  `json:"iat"`
	Expires int64  `json:"exp"`
}

const issuer = "mailer"

// The only header a token may have. Comparing it byte for byte rejects
// "alg":"none" and every other algorithm without parsing it.
var tokenHeader = b64.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

var b64 = base64.RawURLEncoding

var errBadToken = errors.New("invalid or expired token")

// sign returns c as an HS256 JSON Web Token.
func sign(key []byte, c Claims) string {
	body, _ := json.Marshal(c)
	unsigned := tokenHeader + "." + b64.EncodeToString(body)
	return unsigned + "." + b64.EncodeToString(mac(key, unsigned))
}

// parse checks a token's header, signature, issuer and lifetime.
func parse(key []byte, token string, now time.Time) (Claims, error) {
	var c Claims
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != tokenHeader {
		return c, errBadToken
	}
	sig, err := b64.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac(key, parts[0]+"."+parts[1])) {
		return c, errBadToken
	}
	body, err := b64.DecodeString(parts[1])
	if err != nil || json.Unmarshal(body, &c) != nil {
		return c, errBadToken
	}
	// A minute's grace on iat covers clock steps on the host.
	if c.Issuer != issuer || now.Unix() >= c.Expires || c.Issued > now.Add(time.Minute).Unix() {
		return c, errBadToken
	}
	return c, nil
}

func mac(key []byte, s string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(s))
	return h.Sum(nil)
}
