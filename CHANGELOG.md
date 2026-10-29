# Changelog

Notable changes to Mailer. Versions follow [Semantic Versioning](https://semver.org/).

## Unreleased

The first release.

### Added

- Mail server for your own domains in one container: IMAP and SMTP submission for
  mail apps, inbound mail through Cloudflare Email Routing, sending through
  Cloudflare Email Sending with DKIM.
- Automatic Cloudflare setup for every domain on the account: Email Routing, the
  inbound Worker, Email Sending, and DKIM, DMARC and SPF records.
- No open ports: inbound mail through a Cloudflare Tunnel, mail apps through
  Tailscale Funnel, both run inside the container.
- Autoconfig and Autodiscover for Outlook and Thunderbird.
- Web dashboard for accounts, aliases and catch-alls, the queue, logs and a
  per-account sign-in record with a test sign-in, optionally served through the
  tunnel behind device approval and dashboard accounts.
- A Docs page in the dashboard with every guide.
- Sign-in limits per address and per mail account.
- Installers for Linux, macOS and Windows, and a multi-platform image at
  `ghcr.io/krispowers/mailer`.
