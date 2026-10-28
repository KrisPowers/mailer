# Dashboard

The dashboard is at <http://localhost:8080> on the computer running Docker. The
header has search (Ctrl+K), mail app settings, re-check, settings and **New
account**. The status bar along the bottom shows whether the server is running,
and any failing checks with the button that fixes each.

| Page | What it's for |
|---|---|
| **Accounts** | The accounts table. Each account's page has its IMAP and SMTP settings (copy all, print), controls to lock receiving, sending or sign-in, its aliases and catch-all, folders, and sign-ins with a test |
| **Queue** | All mail, received and sent: waiting, delivered, junk, rejected, sent, bounced |
| **Logs** | Recent server activity |
| **Settings** | DMARC strictness, saved sent copies, message size, the Cloudflare SMTP token, Cloudflare and Tailscale connections |
