<img src="docs/images/header.jpg" width="100%" alt="Mailer. Requires Docker with Compose v2, a Cloudflare account on the Workers Paid plan, and a free Tailscale account.">

<a href="docs/how-it-works.md"><img src="docs/images/how-it-works.jpg" width="100%" alt="How it works: scanners on the internet are refused on ports 25, 143 and 993 because every port on your network is closed. Senders reach Cloudflare Email Routing, which hands mail to Mailer through the tunnel. Mail apps connect through Tailscale Funnel on IMAP 443 and SMTP 8443, after autodiscover finds the settings through the tunnel. Replies go out through Cloudflare Email Sending to recipients, DKIM-signed."></a>

<a href="docs/configuration.md#dashboard-access"><img src="docs/images/setup-and-access.jpg" width="100%" alt="Sets itself up: connecting Cloudflare once sets up Routing, the Worker, Sending, DKIM, DMARC and SPF for every domain on the account, including domains added later. Dashboard from anywhere: approved devices reach the dashboard sign-in, and a new browser waits with its code until an admin approves it."></a>

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

## License

[MIT](LICENSE)
