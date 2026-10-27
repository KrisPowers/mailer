package cloudflare

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"mailer/internal/smtpclient"
)

// Relay delivers outbound mail through Cloudflare Email Sending when that's
// turned on, and directly to the recipient's MX otherwise. Messages Email
// Sending can't take (over 5 MiB, or not UTF-8, since the API carries the
// message as a JSON string) go direct.
type Relay struct {
	Setup  *Setup
	Direct *smtpclient.Client
}

func (r *Relay) Deliver(ctx context.Context, domain, from string, rcpts []string, data []byte) []smtpclient.Result {
	cfg := r.Setup.Cfg.Get()
	z, ok := cfg.Cloudflare.Zones[strings.ToLower(from[strings.LastIndexByte(from, '@')+1:])]
	if !cfg.SendViaCloudflare || !ok || len(data) > MaxSendBytes || !utf8.Valid(data) {
		return r.Direct.Deliver(ctx, domain, from, rcpts, data)
	}
	c, err := r.Setup.client(ctx)
	var res *SendResult
	if err == nil {
		res, err = c.SendRaw(ctx, z.AccountID, from, rcpts, string(data))
	}
	out := make([]smtpclient.Result, len(rcpts))
	for i, rcpt := range rcpts {
		out[i].Rcpt = rcpt
		switch {
		case err != nil:
			out[i].Err = fmt.Errorf("Cloudflare Email Sending: %w", err)
		case slices.ContainsFunc(res.PermanentBounces, func(a string) bool { return strings.EqualFold(a, rcpt) }):
			out[i].Err, out[i].Permanent = errors.New("rejected by the recipient's server (via Cloudflare Email Sending)"), true
		}
	}
	return out
}
