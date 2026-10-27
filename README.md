# Mailer

A self-hosted mail server for your own domains. Outlook or the Gmail app is the UI;
this program stores, receives, and sends the mail. No router ports are opened.

```
Internet mail ──▶ Cloudflare Email Routing ──▶ Worker ──▶ Cloudflare Tunnel ──▶ mailer
Outlook / Gmail app ──▶ Tailscale Funnel (:443 IMAP, :8443 SMTP) ─────────────▶ mailer
Your replies ◀── mailer sends directly, DKIM-signed
```

## Setup

1. **Build:** `go build -o mailer ./cmd/mailer`. Build for another OS with, for example,
   `GOOS=windows GOARCH=amd64 go build -o mailer.exe ./cmd/mailer`.
2. **Install [Tailscale](https://tailscale.com/download)** on the server and sign in.
3. **Run `mailer`.** Then open **http://127.0.0.1:8080** on the server and:
   - paste a Cloudflare API token (the page lists the permissions) → **Connect**
   - **Tailscale → Enable**. The first time, Tailscale may show one link to allow Funnel.
   - **Accounts → Create account**
   - more domains: **Domains → Add domain** (the token must be able to see them:
     list each under Zone Resources, or pick All zones)

To start it automatically at boot: `sudo mailer install` (on Windows, run from an
Administrator terminal).

The page then shows the server address and ports to enter in Outlook or the Gmail app.

## What the setup does for you

- Cloudflare, per domain: deploys the email Worker, turns on Email Routing, points the
  catch-all at the Worker, and publishes the DKIM, DMARC, and SPF records. One tunnel
  serves every domain; the first domain connected (the primary) carries the server's
  hostname and the tunnel's address
- Downloads `cloudflared` (official release, checksum-verified) and keeps it running
- Keeps every domain's SPF updated when your home IP changes
- Tailscale Funnel: IMAP on port 443 and sending on port 8443, with Tailscale's TLS
  certificate and the real client IP passed through for rate limiting

## Sending

By default replies go straight from this machine to the recipient's mail server on
port 25, DKIM-signed, with your home IP published in each domain's SPF. Many home
ISPs block port 25, and some receivers junk mail from home connections. If the
Overview shows port 25 blocked, turn on **Settings → Send through Cloudflare**: mail
is handed to [Cloudflare Email Sending](https://developers.cloudflare.com/email-service/)
instead, which onboards every domain and publishes its records. It needs the Workers
Paid plan and the token's **Email Sending · Edit** permission. Messages over 5 MB
still go direct.

## Admin page

The **Overview** page shows the connection checks (tunnel, Email Routing, inbound path,
Funnel, IMAP reachability, outbound port 25) and your client settings. The other pages:

| Page | What it's for |
|---|---|
| **Domains** | Add and remove domains; per-domain Email Routing, catch-all, MX, SPF, DKIM and DMARC checks read from Cloudflare's API; repair |
| **Accounts** | Create accounts, see per-account mail counts and sizes, reset passwords, delete accounts |
| **Aliases** | Extra addresses and a catch-all per domain |
| **Outbound queue** | Messages waiting to send; retry them |
| **Logs** | Recent server activity |
| **Settings** | DMARC strictness, saving sent copies, max message size, re-running Cloudflare setup |

The admin page only listens on 127.0.0.1, so it can only be reached from the server itself.

## Data

Everything lives in `mailer-data/` next to the binary: mail, accounts, keys, and
settings. Back that folder up. Use `-data DIR` to put it somewhere else.

## Limits

- 25 MB per message (Cloudflare Email Routing's limit)
- Each received email uses one Workers request, from your Cloudflare account's free daily allowance

## Development

```sh
go test ./...
MAILER_NET_TESTS=1 go test ./internal/tunnel   # also tests the real cloudflared download
```

### Admin UI

The admin page is React + TypeScript, built with Vite, in `internal/admin/ui`.
The build goes to `internal/admin/web`, which is committed and embedded in the
binary, so building the server never needs Node.

```sh
cd internal/admin/ui
npm install
npm run dev     # hot reload on :5173, proxies /api to a running mailer on :8080
npm run build   # type-check, then rebuild internal/admin/web; commit the result
```

CI rebuilds the UI on every PR, fails if the committed build is stale, and uploads
the fresh build as the `web` artifact.
