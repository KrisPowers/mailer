import { CopyButton } from "../components/CopyButton";
import { useOverview } from "../overview";

// ClientSettings lists what to type into Outlook or the Gmail app.
export function ClientSettings() {
  const { ov } = useOverview();
  const c = ov?.client;
  if (!c) return <p className="muted">Available once Tailscale is enabled.</p>;
  return (
    <dl className="kv">
      <dt>Account type</dt><dd>IMAP</dd><span />
      <dt>Incoming server</dt><dd className="mono">{c.server}</dd><CopyButton text={c.server} label="Server copied" />
      <dt>Incoming port</dt><dd>{c.imap_port} · SSL/TLS</dd><span />
      <dt>Outgoing server</dt><dd className="mono">{c.server}</dd><CopyButton text={c.server} label="Server copied" />
      <dt>Outgoing port</dt><dd>{c.smtp_port} · SSL/TLS</dd><span />
      <dt>Username</dt><dd>full email address</dd><span />
    </dl>
  );
}
