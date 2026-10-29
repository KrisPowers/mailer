# Dashboard

The dashboard is at <http://localhost:8080> on the computer running Docker, and at
`<address>.<primary domain>` (default `mail-admin`) when remote access is on.
Before it opens, a browser needs an approved device and a signed-in account (see
[Configuration](configuration.md#dashboard-access)).

The header has search (Ctrl+K), mail app settings, re-check, settings, the
signed-in account with **Sign out**, and **New account**. The status bar along the
bottom shows whether the server is running, and any failing checks with the button
that fixes each.

| Page | What it's for |
|---|---|
| **Accounts** | The accounts table. Each account's page has its IMAP and SMTP settings (copy all, print), controls to lock receiving, sending or sign-in, its aliases and catch-all, folders, and sign-ins with a test |
| **Queue** | All mail, received and sent: waiting, delivered, junk, rejected, sent, bounced |
| **Logs** | Recent server activity |
| **Docs** | These guides: how it works, configuration, the dashboard and troubleshooting |
| **Access** | Admins only. **Devices**: approve, name or remove the browsers allowed to reach the sign-in. **Dashboard accounts**: make admins and users, set passwords, turn sign-in off. **Sessions**: everyone signed in now, each can be ended |
| **Settings** | DMARC strictness, saved sent copies, message size, the Cloudflare SMTP token, Cloudflare and Tailscale connections, **Server** (admins: Tailscale name and auth key, remote access, debugging) and **Security** (your password) |
