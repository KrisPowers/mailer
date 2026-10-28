# Configuration

Most settings live in the dashboard (**Settings**, the gear at the top right):
DMARC strictness, saved sent copies, message size, and the Cloudflare and
Tailscale connections.

## Environment

Container settings go in a `.env` file next to `compose.yaml`. It is git-ignored,
so keys put there stay out of the repository. [`.env.example`](../.env.example)
lists every one.

| Variable | Default | What it does |
|---|---|---|
| `MAILER_DATA_DIR` | `%LOCALAPPDATA%\mailer\data` on Windows, `./mailer/data` elsewhere | Host folder for mail, accounts, keys and settings. The installer sets `./data` |
| `TS_HOSTNAME` | `mail` | This server's name on your tailnet. Mail apps connect to `<name>.<tailnet>.ts.net` |
| `TS_AUTHKEY` | empty | A [Tailscale auth key](https://tailscale.com/kb/1085/auth-keys) that signs the server in without the dashboard link |
| `MAILER_TRACE` | `0` | `1` logs every command mail apps send, with passwords hidden |
| `MAILER_IMAP_CAPS` | empty | Overrides the IMAP extensions advertised, to find one a mail app can't handle |

Apply a change with `docker compose up -d` in the folder holding `compose.yaml`.

## Ports

`compose.yaml` publishes two ports on `127.0.0.1` only:

- `8080`: the dashboard
- `8877`: where Cloudflare's sign-in comes back to

Mail ports are never published. The internet reaches them through the Cloudflare
Tunnel and Tailscale Funnel inside the container.

## Data and backups

Mail, accounts, DKIM keys, settings (including the Cloudflare credentials), the
sign-in record (`signins.log`) and Tailscale's state all live in the container's
`/data`, mapped to `MAILER_DATA_DIR` on the host. Back that folder up, and treat
it as secret.
