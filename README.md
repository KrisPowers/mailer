# Mailer

A self-hosted mail server for your own domains. Outlook or the Gmail app is the UI;
this program stores, receives, and sends the mail. No router ports are opened.

```
Internet mail ──▶ Cloudflare Email Routing ──▶ Worker ──▶ Cloudflare Tunnel ──▶ mailer
Outlook / Gmail app ──▶ Tailscale Funnel (:443 IMAP, :8443 SMTP) ─────────────▶ mailer
Your replies ──▶ mailer ──▶ Cloudflare Email Sending ──▶ recipient (DKIM-signed)
```

## Setup

Mailer runs as one Docker container: the mail server with its web dashboard,
`cloudflared` for the Cloudflare Tunnel, and Tailscale for Funnel. Nothing on your
router or computer is opened; the dashboard is published to this computer only.

1. **Start it** from the repo folder (Docker Desktop shows it as `mailer` afterwards):
   ```sh
   docker compose up -d --build
   ```
2. **Open http://localhost:8080** and:
   - **Connect Cloudflare**, then approve in the tab that opens. Mailer signs in
     through Cloudflare's own `cf` command-line app, keeps the sign-in renewed, and
     sets everything up by itself.
   - **Tailscale → Enable**. The first time it shows a link to sign this server in
     to your tailnet, then one to allow Funnel; click Enable again after each.
   - **Accounts → Create account**
   - more domains: **Domains → Add domain** (any domain on your Cloudflare account)

The dashboard then shows the server address and ports to enter in Outlook or the
Gmail app. The container restarts by itself with Docker.

Set `TS_AUTHKEY` (a [Tailscale auth key](https://tailscale.com/kb/1085/auth-keys))
before starting to sign Tailscale in without the link. `TS_HOSTNAME` in
`compose.yaml` names the server on your tailnet (default `mail`).

### Moving from the desktop app

On Windows the container uses the desktop app's data folder,
`%LOCALAPPDATA%\mailer\data`, so accounts, mail and the Cloudflare setup carry over.
Quit the desktop app (**Mailer → Quit Mailer**) before starting the container. The
container is a new machine on your tailnet, so enable Funnel once from the dashboard
and point mail apps at the new server name it shows.

## What the setup does for you

- Cloudflare, per domain: deploys the email Worker, turns on Email Routing, points the
  catch-all at the Worker, onboards the domain for Email Sending, and publishes the
  DKIM, DMARC, and SPF records. One tunnel
  serves every domain; the first domain connected (the primary) carries the server's
  hostname and the tunnel's address
- Keeps `cloudflared` (in the image) connected
- Mail app settings: `autodiscover.<domain>` (Outlook) and `autoconfig.<domain>`
  (Thunderbird and others) are routed through the tunnel and answer with the server
  and ports, so a mail app needs only the address and password
- Tailscale Funnel: IMAP on port 443 and sending on port 8443, with Tailscale's TLS
  certificate and the real client IP passed through for rate limiting

## Sending

Outgoing mail is handed to [Cloudflare Email Sending](https://developers.cloudflare.com/email-service/),
part of Cloudflare Email Service. Nothing is sent from this machine and no port 25 is
used or opened, and your home IP is never published in DNS. Email Sending needs the
Workers Paid plan ($5/month) on the Cloudflare account. Setup onboards every domain
and publishes the records it asks for.

Cloudflare takes messages up to 5 MB; a larger message bounces back to the sender with
the reason. Messages Cloudflare can't take yet (for example, before the plan is active)
wait in the **Outbound queue** and are retried for four days.

## The dashboard

The **Overview** page shows the connection checks (tunnel, Email Routing, inbound path,
Funnel, IMAP reachability, Email Sending) and your client settings. The other pages:

| Page | What it's for |
|---|---|
| **Domains** | Add and remove domains; per-domain Email Routing, catch-all, MX, SPF, DKIM and DMARC checks read from Cloudflare's API; repair |
| **Accounts** | Create accounts, see per-account mail counts and sizes, reset passwords, delete accounts |
| **Aliases** | Extra addresses and a catch-all per domain |
| **Outbound queue** | Messages waiting to send; retry them |
| **Logs** | Recent server activity |
| **Settings** | DMARC strictness, saving sent copies, max message size, re-running Cloudflare setup |

`compose.yaml` publishes the dashboard on 127.0.0.1 only, so it can only be reached
from the computer running Docker.

## Data

Mail, accounts, keys, settings and Tailscale's state live in the container's `/data`,
which `compose.yaml` maps to `%LOCALAPPDATA%\mailer\data` on Windows and
`./mailer/data` elsewhere. Back that folder up.

## Limits

- 25 MB per message (Cloudflare Email Routing's limit)
- Each received email uses one Workers request, from your Cloudflare account's free daily allowance

## Development

```sh
go test ./...
MAILER_NET_TESTS=1 go test ./internal/tunnel   # also tests the real cloudflared download
```

### Dashboard

The dashboard is React + TypeScript, built with Vite, in `internal/admin/ui`.
The image builds it itself. A copy in `internal/admin/web` is committed so `go vet`,
`go test` and builds outside Docker work without Node; `scripts\build.ps1` (or
`npm run build`) refreshes it.

```sh
cd internal/admin/ui
npm install
npm run dev     # hot reload on :5173, proxies /api to a running mailer on :8080
npm run build   # type-check, then rebuild internal/admin/web; commit the result
```
