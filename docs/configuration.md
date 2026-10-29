# Configuration

There's no `.env` file. Everything is set in the dashboard and kept in the data
folder:

- The **first-run setup**, on the computer running Mailer, makes the admin account
  and takes the Tailscale name, an optional Tailscale auth key and whether the
  dashboard can be opened from anywhere.
- **Settings** (the gear at the top right) has DMARC strictness, saved sent copies,
  message size, the Cloudflare and Tailscale connections, and, for admins,
  **Server**: the same Tailscale and remote access settings, and two switches for
  debugging mail apps.

| Setting | Where | What it does |
|---|---|---|
| Name on your tailnet | Setup, Settings, Server | Mail apps connect to `<name>.<tailnet>.ts.net`. Default `mail` |
| Tailscale auth key | Setup, Settings, Server | A [Tailscale auth key](https://tailscale.com/kb/1085/auth-keys) that signs the server in without the dashboard link. Never shown again once saved |
| Open the dashboard from anywhere | Setup, Settings, Server | Serves the dashboard at `<address>.<primary domain>` through the Cloudflare Tunnel. Default address `mail-admin` |
| Log every mail app command | Settings, Server | Logs every command mail apps send, with passwords hidden |
| IMAP extensions advertised | Settings, Server | Overrides the IMAP extensions advertised, to find one a mail app can't handle |

## Dashboard access

Every visit to the dashboard, on this computer or through the tunnel, needs two
things:

1. **An approved device.** Each browser gets a random device ID on its first visit,
   kept in its local storage, and sees only a waiting screen with a short code
   until an admin approves that code under **Access**, **Devices**. The browser
   used for setup is approved by it.
2. **An account.** There's no sign-up; admins make accounts under **Access**,
   **Dashboard accounts**, as an admin or a user. Users manage mail; admins also
   approve devices, manage accounts and change **Server** settings.

Signing in gives a token that lasts 15 minutes and is renewed while the page is
open, bound to that session and device: removing the device, turning the account
off or changing its password signs it out at once. A session ends after a week
unused and after 30 days regardless. Five wrong passwords in 15 minutes lock out
that address and that username for 15 minutes.

Through the tunnel, setup is refused: it can only be done on the computer running
Mailer. To start it again, for example after losing every admin password, stop
the container, delete `auth.json` from the data folder and start it again; mail,
accounts and settings are kept.

## Ports

`compose.yaml` publishes two ports on `127.0.0.1` only:

- `8080`: the dashboard
- `8877`: where Cloudflare's sign-in comes back to

Mail ports are never published. The internet reaches them, and the dashboard when
remote access is on, through the Cloudflare Tunnel and Tailscale Funnel inside the
container.

## Data and backups

Mail, accounts, DKIM keys, settings (including the Cloudflare credentials and the
Tailscale auth key), dashboard accounts and devices (`auth.json`), the sign-in
record (`signins.log`) and Tailscale's state all live in the container's `/data`,
mapped to `%LOCALAPPDATA%\mailer\data` on Windows and `mailer/data` next to
`compose.yaml` elsewhere. Back that folder up, and treat it as secret.
