# Mailer

[![Release](https://github.com/KrisPowers/mailer/actions/workflows/release.yml/badge.svg)](https://github.com/KrisPowers/mailer/actions/workflows/release.yml)
[![CodeQL](https://github.com/KrisPowers/mailer/actions/workflows/codeql.yml/badge.svg)](https://github.com/KrisPowers/mailer/actions/workflows/codeql.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Self-hosted mail, zero ports.

Mailer is a mail server for your own domains that runs in one Docker container.
Outlook, the Gmail app or Thunderbird is the inbox; Mailer stores, receives and
sends the mail. Nothing on your router is opened and your home IP never appears
in DNS.

```text
Internet mail ──▶ Cloudflare Email Routing ──▶ Worker ──▶ Cloudflare Tunnel ──▶ mailer
Mail apps     ──▶ Tailscale Funnel (:443 IMAP, :8443 SMTP) ─────────────────▶ mailer
Your replies  ──▶ mailer ──▶ Cloudflare Email Sending ──▶ recipient (DKIM-signed)
```

## Features

- **Set up by itself.** Connect Cloudflare once and every domain on the account
  gets Email Routing, the inbound Worker, Email Sending, and DKIM, DMARC and SPF
  records. Domains added to Cloudflare later are picked up automatically.
- **No open ports.** Inbound mail comes through a Cloudflare Tunnel and mail apps
  connect through Tailscale Funnel, both run inside the container.
- **Mail app autoconfig.** Outlook and Thunderbird need only the address and
  password.
- **Web dashboard, from anywhere.** Accounts, aliases and catch-alls, the mail
  queue, logs, and a per-account sign-in record with a one-click test. It can be
  served through the Cloudflare Tunnel; every visit needs an account an admin made
  and a device an admin approved.

## Requirements

- Docker with Compose v2 ([Docker Desktop](https://docs.docker.com/get-docker/)
  on Windows and macOS)
- A [Cloudflare](https://dash.cloudflare.com/) account with your domains, on the
  Workers Paid plan for sending
- A [Tailscale](https://tailscale.com/) account

## Install

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/KrisPowers/Mailer/main/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/KrisPowers/Mailer/main/scripts/install.ps1 | iex
```

The installer puts `compose.yaml` in `~/.mailer` (Windows: `%LOCALAPPDATA%\mailer`),
pulls the image and starts the container. Run it again to update. There's no
`.env`: settings are made in the dashboard and kept in the data folder.

From a checkout instead:

```sh
git clone https://github.com/KrisPowers/Mailer.git
cd Mailer
docker compose up -d --build
```

## Get started

Open <http://localhost:8080> on the computer running Mailer, then:

1. **First-run setup.** Make the admin account, give the server its name on your
   tailnet (and a Tailscale auth key, to skip a sign-in link later) and choose
   whether the dashboard can be opened from anywhere. This browser is approved as
   the first device and signed in.
2. **Connect Cloudflare** and approve in the tab that opens. Mailer signs in
   through Cloudflare's `cf` command-line app, keeps the sign-in renewed, and sets
   up every domain on the account.
3. **Tailscale, Enable.** Without an auth key it first shows a link to sign this
   server in to your tailnet, then one to allow Funnel; click Enable again after
   each.
4. **New account** (top right). The account's page lists the settings to enter in
   your mail app.

To use the dashboard from another device, open it there (on
`mail-admin.<your domain>` with remote access on), note the code it shows, and
approve that code under **Access**, **Devices**. Admins make the accounts that
sign in under **Access**, **Dashboard accounts**.

## Documentation

| Guide | What's in it |
|---|---|
| [How it works](docs/how-it-works.md) | What setup does, sending, limits |
| [Configuration](docs/configuration.md) | Settings, dashboard access, data folder, backups |
| [Dashboard](docs/dashboard.md) | Pages and controls |
| [Troubleshooting](docs/troubleshooting.md) | When a mail app can't sign in |
| [Development](docs/development.md) | Building, testing, the dashboard UI, CI and releases |

Changes are listed in the [changelog](CHANGELOG.md).

## Contributing

Issues and pull requests are welcome; see [CONTRIBUTING](.github/CONTRIBUTING.md).
Report security problems privately as described in [SECURITY](.github/SECURITY.md).

## License

[MIT](LICENSE)
