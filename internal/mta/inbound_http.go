package mta

import (
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strings"

	"mailer/internal/message"
	"mailer/internal/smtp"
)

// InboundHandler serves the endpoint the Cloudflare Email Worker posts each
// received message to (through the Cloudflare Tunnel):
//
//	POST /inbound   Authorization: Bearer <secret>
//	                X-Mail-From: <envelope sender>
//	                X-Rcpt-To: <envelope recipient>
//	                body: the raw message
//
// 200 means stored. 550 means reject permanently (the Worker calls
// setReject). Anything else is temporary.
func (m *MTA) InboundHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", m.clientConfig)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "mailer ok")
	})
	mux.HandleFunc("POST /inbound", func(w http.ResponseWriter, r *http.Request) {
		cfg := m.Cfg.Get()
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if cfg.Cloudflare.InboundSecret == "" ||
			subtle.ConstantTimeCompare([]byte(auth), []byte(cfg.Cloudflare.InboundSecret)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		rcpt := strings.TrimSpace(r.Header.Get("X-Rcpt-To"))
		if rcpt == "" {
			http.Error(w, "missing recipient", http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cfg.MaxMessageBytes+1))
		if err != nil {
			http.Error(w, "message too large", 552)
			return
		}
		data := message.NormalizeCRLF(body)
		ip, helo, spf := relayInfo(data)
		err = m.Receive(&InboundMessage{
			RemoteIP: ip, Helo: helo,
			From: strings.TrimSpace(r.Header.Get("X-Mail-From")),
			To:   []string{rcpt}, Data: data,
			Via: "Cloudflare Email Routing",
		}, spf)
		var se *smtp.Error
		switch {
		case err == nil:
			io.WriteString(w, "stored")
		case errors.As(err, &se) && se.Code >= 500:
			http.Error(w, se.Msg, 550)
		default:
			http.Error(w, "temporary failure", http.StatusServiceUnavailable)
		}
	})
	return mux
}
