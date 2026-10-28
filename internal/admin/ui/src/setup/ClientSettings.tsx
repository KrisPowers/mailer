import { CopyButton } from "../components/CopyButton";
import { useOverview } from "../overview";

// ClientSettings lists what to type into Outlook or the Gmail app.
export function ClientSettings() {
  const { ov } = useOverview();
  const c = ov?.client;
  if (!c) return <p className="muted">Available once Tailscale is enabled.</p>;
  return (<>
    <dl className="kv">
      <dt>Account type</dt><dd>IMAP</dd><span />
      <dt>Incoming server</dt><dd className="mono">{c.server}</dd><CopyButton text={c.server} label="Server copied" />
      <dt>Incoming port</dt><dd>{c.imap_port} · SSL/TLS</dd><span />
      <dt>Outgoing server</dt><dd className="mono">{c.smtp_server}</dd><CopyButton text={c.smtp_server} label="Server copied" />
      <dt>Outgoing port</dt><dd>{c.smtp_port} · SSL/TLS</dd><span />
      <dt>Username</dt><dd>full email address</dd><span />
      <dt>Password</dt><dd>the one set on the Accounts page</dd><span />
      {c.smtp_user && <>
        <dt>Outgoing username</dt><dd className="mono">{c.smtp_user}</dd><CopyButton text={c.smtp_user} label="Username copied" />
        <dt>Outgoing password</dt><dd>Cloudflare SMTP token</dd><CopyButton text={c.smtp_password ?? ""} label="Token copied" />
      </>}
    </dl>
    <p className="muted small">
      In Outlook, add the account in the Outlook app (not outlook.com, which can't add other mail servers).
      If it opens a Microsoft sign-in page, go back and choose <b>IMAP</b> as the account type, then enter these settings.
      Both ports use SSL/TLS from the start, not STARTTLS.
      {c.smtp_user && " Outgoing mail signs in to Cloudflare with its own username and token, not the account's."}
    </p>
    <p className="muted small">
      Gmail on the web no longer checks other mail accounts (Google ended that in 2026). Use the Gmail app instead:
      <b> Add account → Other</b>, then <b>Personal (IMAP)</b>.
    </p>
    <p className="muted small">
      Still can't sign in? <a href="#signins">Sign-ins</a> shows every attempt and why it failed, and can test an
      account through this same address.
    </p>
  </>);
}
