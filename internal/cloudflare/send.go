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

// Relay delivers outbound mail through Cloudflare Email Sending, so nothing
// is ever sent from this machine on port 25. Messages Email Sending can't
// take (over 5 MiB, or not UTF-8, since the API carries the message as a
// JSON string) fail permanently and bounce back to the sender.
type Relay struct {
	Setup *Setup
}

func (r *Relay) Deliver(ctx context.Context, domain, from string, rcpts []string, data []byte) []smtpclient.Result {
	out := make([]smtpclient.Result, len(rcpts))
	fail := func(err error, permanent bool) []smtpclient.Result {
		for i, rcpt := range rcpts {
			out[i] = smtpclient.Result{Rcpt: rcpt, Err: err, Permanent: permanent}
		}
		return out
	}
	sender := strings.ToLower(from[strings.LastIndexByte(from, '@')+1:])
	z, ok := r.Setup.Cfg.Get().Cloudflare.Zones[sender]
	switch {
	case !ok:
		return fail(fmt.Errorf("%s isn't connected to Cloudflare, so it can't send", sender), true)
	case len(data) > MaxSendBytes:
		return fail(fmt.Errorf("message is %d KB; Cloudflare Email Sending takes up to %d KB", len(data)>>10, MaxSendBytes>>10), true)
	case !utf8.Valid(data):
		return fail(errors.New("message isn't UTF-8, which Cloudflare Email Sending requires"), true)
	}
	c, err := r.Setup.client(ctx)
	var res *SendResult
	if err == nil {
		res, err = c.SendRaw(ctx, z.AccountID, from, rcpts, string(data))
	}
	if err != nil {
		return fail(fmt.Errorf("Cloudflare Email Sending: %w", err), false)
	}
	for i, rcpt := range rcpts {
		out[i].Rcpt = rcpt
		if slices.ContainsFunc(res.PermanentBounces, func(a string) bool { return strings.EqualFold(a, rcpt) }) {
			out[i].Err, out[i].Permanent = errors.New("rejected by the recipient's server (via Cloudflare Email Sending)"), true
		}
	}
	return out
}
