import { CopyButton } from "../components/CopyButton";
import { useOverview } from "../overview";

// ClientSettings lists what to type into Outlook or the Gmail app, the
// relay token included. Each account's own sheet is under Accounts.
export function ClientSettings() {
  const { ov } = useOverview();
  const c = ov?.client;
  if (!c) return <p className="muted">Available once Tailscale is enabled.</p>;
  const row = (label: string, value: string, mono = true) => [
    <dt key={label + "t"}>{label}</dt>,
    <dd key={label + "d"} className={mono ? "mono" : ""}>{value}</dd>,
    mono ? <CopyButton key={label + "c"} text={value} label={label + " copied"} /> : <span key={label + "c"} />,
  ];
  return (<>
    <h3 className="section-label">Incoming mail (IMAP)</h3>
    <dl className="kv">
      {row("Server", c.server)}
      {row("Port", c.imap_port + " · SSL/TLS", false)}
      {row("Username", "the full email address", false)}
      {row("Password", "the account's password", false)}
    </dl>
    <h3 className="section-label">Outgoing mail (SMTP)</h3>
    <dl className="kv">
      {row("Server", c.smtp_server)}
      {row("Port", c.smtp_port + " · SSL/TLS", false)}
      {c.smtp_user ? <>
        {row("Username", c.smtp_user)}
        {row("Password", c.smtp_password ?? "")}
      </> : <>
        {row("Username", "the full email address", false)}
        {row("Password", "the account's password", false)}
      </>}
    </dl>
    <p className="muted small">
      Add it in the Outlook app as an IMAP account (outlook.com can't add other mail servers). Both use SSL/TLS
      from the start, not STARTTLS. Gmail on the web no longer checks other accounts; use the Gmail app's
      <b> Add account → Other</b>. <a href="#signins">Sign-ins</a> shows every attempt and why it failed.
    </p>
  </>);
}
