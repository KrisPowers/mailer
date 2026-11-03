# Dashboard

The admin dashboard is at <http://localhost:8080> on the computer running Docker,
and at `<address>.<primary domain>` (default `mail-admin`) when remote access is
on. Before it opens, a browser needs an approved device and a signed-in account
(see [Configuration](configuration.md#dashboard-access)).

The header has search (Ctrl+K) for accounts and pages, **New account**, **Help**,
**Settings**, and the signed-in account with **Sign out**. The sidebar has the
pages, with a count beside each one that has something waiting.

| Page | What it's for |
|---|---|
| **Accounts** | Every mailbox. Each account's page has its access switches (receive, send, sign in, and **Mail apps**, which lets Outlook and other IMAP apps sign in and is off for new accounts), a new password, its mail app settings (copy all, print), its aliases and catch-all, its folders, and its sign-ins with a test |
| **Traffic** | All mail, received and sent: waiting, delivered, junk, rejected, sent, bounced, with **Retry now** for mail waiting to leave |
| **Flagged** | Every flagged message in every account: flagged by its reader, or by Mailer on delivery for a risky attachment or as marketing. Clear the flag or move the message to Trash |
| **Status** | The Cloudflare, Tailscale and sending checks, run every minute, with **Repair** and **Enable Funnel** beside the checks they fix |
| **Logs** | Recent server activity, with a filter and errors only |
| **Devices** | Admins only. Approve, name or remove the browsers allowed to reach the sign-in |
| **People** | Admins only. The accounts that sign in to the dashboard, as admins or users: make them, set passwords, turn sign-in off. Below, everyone signed in now, each session can be ended |
| **Settings** | Mail handling (DMARC strictness, sent copies, message size), Sending (the Cloudflare SMTP token), Connections (Cloudflare and Tailscale), **Server** (admins: Tailscale name and auth key, remote access to the dashboard and the [webmail](webmail.md), debugging), Appearance, and Account (your password and sessions) |
| **Help** | These guides: how it works, configuration, the dashboard, the webmail and troubleshooting |

Until Cloudflare, Tailscale and a first account are set up, the sidebar has
**Finish setup**, which walks through them.
