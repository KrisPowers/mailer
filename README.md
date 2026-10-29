# Mailer

Self-hosted mail, zero ports.

<a href="docs/how-it-works.md"><img src="docs/images/flow.jpg" width="100%" alt="How mail moves: senders reach Cloudflare, which passes mail through its tunnel to Mailer; mail apps connect through Tailscale Funnel; replies go out through Cloudflare Email Sending, DKIM-signed. No ports are open."></a>

<a href="docs/how-it-works.md#what-setup-does"><img src="docs/images/setup.jpg" width="100%" alt="Sets itself up: connecting Cloudflare once sets up Routing, the Worker, Sending, DKIM, DMARC and SPF for every domain, including domains added later."></a>

## Requirements

- Docker with Compose v2 ([Docker Desktop](https://docs.docker.com/get-docker/)
  on Windows and macOS)
- A [Cloudflare](https://dash.cloudflare.com/) account with your domains, on the
  Workers Paid plan for sending
- A [Tailscale](https://tailscale.com/) account

## Install

Homebrew (macOS and Linux):

```sh
brew tap krispowers/mailer https://github.com/KrisPowers/mailer
brew install krispowers/mailer/mailer
mailer start
```

`mailer update` updates it; `mailer stop`, `status`, `logs` and `open` do what
they say.

Linux and macOS, without Homebrew:

```sh
curl -fsSL https://raw.githubusercontent.com/KrisPowers/mailer/main/scripts/install.sh | sh
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/KrisPowers/mailer/main/scripts/install.ps1 | iex
```

Run it again to update. From a checkout instead:

```sh
git clone https://github.com/KrisPowers/mailer.git
cd mailer
docker compose up -d --build
```

## Get started

Open <http://localhost:8080> on the computer running Mailer:

1. **Setup:** make the admin account and name the server on your tailnet.
2. **Connect Cloudflare** and approve in the tab that opens.
3. **Tailscale, Enable**, and follow the links it shows.
4. **New account** (top right) lists the settings for your mail app.

Every guide is in the dashboard under **Docs**.

## Contributing

Issues and pull requests are welcome; see [CONTRIBUTING](.github/CONTRIBUTING.md).
Report security problems privately as described in [SECURITY](.github/SECURITY.md).

## License

[MIT](LICENSE)
