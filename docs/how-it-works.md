# How it works

Mailer runs as one Docker container: the mail server with its admin dashboard and
webmail, `cloudflared` for the Cloudflare Tunnel, and Tailscale for Funnel. Nothing
on your router or computer is opened; the dashboard is published to `127.0.0.1`, and
served through the tunnel too when remote access is on. Either way it needs an
approved device and a signed-in account (see
[Configuration](configuration.md#dashboard-access)).

## What setup does

- **Cloudflare, per domain:** deploys the email Worker, turns on Email Routing,
  points the catch-all at the Worker, onboards the domain for Email Sending, and
  publishes the DKIM, DMARC and SPF records. One tunnel serves every domain; the
  first domain connected (the primary) carries the server's hostname and the
  tunnel's address.
- **New domains:** every domain on the Cloudflare account is connected at startup
  and every 10 minutes, including domains added later.
- **Tunnel:** keeps `cloudflared` (in the image) connected.
- **Mail app settings:** `autodiscover.<domain>` (Outlook) and
  `autoconfig.<domain>` (Thunderbird and others) are routed through the tunnel and
  answer with the server and ports, so a mail app needs only the address and
  password.
- **Tailscale Funnel:** IMAP on port 443 and sending on port 8443, with
  Tailscale's TLS certificate and the real client IP passed through for rate
  limiting.

## Reading and writing

Each account reads and writes its mail in the [webmail](webmail.md), on its own
port beside the admin dashboard, with folders, signatures, categories, flags and
rules that file mail as it arrives. Outlook and other mail apps reach the same
mailbox over IMAP and SMTP through Tailscale Funnel, for accounts whose **Mail
apps** switch is on.

## Sending

Outgoing mail is handed to
[Cloudflare Email Sending](https://developers.cloudflare.com/email-service/), part
of Cloudflare Email Service. Nothing is sent from your machine, port 25 is never
used, and your home IP is never published in DNS. Email Sending needs the Workers
Paid plan on the Cloudflare account.

Cloudflare takes messages up to 5 MB; a larger message bounces back to the sender
with the reason. Messages Cloudflare can't take yet (for example, before the plan
is active) wait in the queue and are retried for four days.

## Receiving

Cloudflare Email Routing hands each message to the Worker, which posts it through
the tunnel to Mailer. Mailer checks SPF, DKIM and DMARC, applies aliases and
catch-alls, and files it in the account's mailbox.

Mailer also flags mail as it arrives, with keywords mail apps can show:
`$Risky` when an attachment can run code (programs, scripts, shortcuts,
macro-enabled Office files, disk images), and `$Marketing` for bulk mail (a
`List-Unsubscribe` header or `Precedence: bulk`). Those, and messages users flag
in their mail apps, are listed on the dashboard's **Flagged** page.

## Limits

- 25 MB per received message (Cloudflare Email Routing's limit)
- 5 MB per sent message (Cloudflare Email Sending's limit)
- Each received message uses one Workers request from the Cloudflare account's
  daily allowance
- Gmail on the web no longer checks other mail accounts; use the Gmail app's
  **Add account, Other** instead
