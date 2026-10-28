import { useOverview } from "../overview";
import type { ClientSettings } from "../types";
import { Icon } from "../ui/Icon";
import { Modal } from "../ui/Modal";
import { KeyValues, type KV } from "../ui/Page";

// credentialRows lists what a mail app needs, for one address or, without
// one, in general. The account password isn't stored readably, so it's
// described rather than shown unless a fresh one is passed in.
export function credentialRows(c: ClientSettings, email?: string, password?: string) {
  const user: KV = email ? { label: "Username", value: email, mono: true } : { label: "Username", value: "the full email address", copy: false };
  const pw: KV = password ? { label: "Password", value: password, mono: true } : { label: "Password", value: "the account's password", copy: false };
  const imap: KV[] = [
    { label: "Server", value: c.server, mono: true },
    { label: "Port", value: String(c.imap_port), mono: true },
    { label: "Security", value: "SSL/TLS", copy: false },
    user, pw,
  ];
  const smtp: KV[] = [
    { label: "Server", value: c.smtp_server, mono: true },
    { label: "Port", value: String(c.smtp_port), mono: true },
    { label: "Security", value: "SSL/TLS", copy: false },
    ...(c.smtp_user ? [
      { label: "Username", value: c.smtp_user, mono: true },
      { label: "Password", value: c.smtp_password ?? "", mono: true },
    ] : [user, pw]),
  ];
  return { imap, smtp };
}

export function credentialText(c: ClientSettings, email: string, password?: string) {
  const { imap, smtp } = credentialRows(c, email, password);
  const lines = (rows: KV[]) => rows.map((r) => `  ${r.label}: ${r.value}`);
  return [`Account: ${email}`, "", "Incoming mail (IMAP)", ...lines(imap), "", "Outgoing mail (SMTP)", ...lines(smtp)].join("\n");
}

export function CredentialCards({ c, email, password }: { c: ClientSettings; email?: string; password?: string }) {
  const { imap, smtp } = credentialRows(c, email, password);
  return (
    <div className="cards-2">
      <div className="card">
        <h3><Icon name="inbox" />Incoming mail (IMAP)</h3>
        <KeyValues rows={imap} />
      </div>
      <div className="card">
        <h3><Icon name="send" />Outgoing mail (SMTP)</h3>
        <KeyValues rows={smtp} />
      </div>
    </div>
  );
}

// MailSettings is the general sheet, from the header's key button.
export function MailSettings() {
  const { ov } = useOverview();
  const c = ov?.client;
  return (
    <Modal title="Mail app settings" wide>
      {c ? <>
        <CredentialCards c={c} />
        <p className="muted small">Add it in the Outlook app or the Gmail app as an IMAP account. Each account's page lists its own settings,
          with copy all and print.</p>
      </> : <p className="muted">Available once Tailscale Funnel is enabled.</p>}
    </Modal>
  );
}
