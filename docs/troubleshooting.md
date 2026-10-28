# Troubleshooting

## A mail app can't sign in

Open the account and its **Sign-ins** tab. Every IMAP and SMTP sign-in attempt is
listed with the app's name, the address it came from (the real one, passed through
by Funnel) and the result: wrong password, blocked, sign-in locked, not encrypted,
a sign-in method that isn't offered, or an app that connected and left without
trying, with the commands it sent first.

- **Nothing listed** when the app fails: it never reached the server. Check the
  server name and ports it uses against the account's **Credentials** tab.
- **Blocked:** five failures in 15 minutes block that address. Outlook and Gmail
  sign in from their own servers, so one wrong password saved in the app can block
  it; fix the password, then **Unblock** it from that row.
- **Test sign-in**, on the same tab, checks the password and then signs in to IMAP
  and SMTP through the public address, step by step.

For more detail, set `MAILER_TRACE=1` in `.env` and restart with
`docker compose up -d`; every command a mail app sends is logged, with passwords
hidden. See [Configuration](configuration.md).

## Mail isn't being sent

Check **Queue** for messages waiting or bounced. Cloudflare Email Sending needs the
Workers Paid plan; until it's active, messages wait and are retried for four days.

## Logs

The dashboard's **Logs** page shows recent activity. The full log is
`mailer.log` in the data folder, and `docker logs mailer` shows the same.
