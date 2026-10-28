import { useState } from "react";
import { api } from "../api";
import { SetPassword } from "../flows/Accounts";
import { CredentialCards, credentialText } from "../flows/MailSettings";
import { fmtSize, fmtTime } from "../format";
import { useOverview } from "../overview";
import { href } from "../router";
import type { Account } from "../types";
import { Button } from "../ui/Button";
import { Icon } from "../ui/Icon";
import { Confirm, useModal } from "../ui/Modal";
import { Chip, Dot, EmptyRow, Metrics, PageHeader, Tabs } from "../ui/Page";
import { useCopy, useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";
import { RESULTS } from "./SignIns";

type Tab = "credentials" | "folders" | "signins";

// AccountPage is one account, like a container's page in Docker Desktop:
// what a mail app needs, what the mailbox holds, and its sign-ins.
export function AccountPage({ account: a, onChanged }: { account: Account; onChanged: () => Promise<void> }) {
  const { open } = useModal();
  const toast = useToast();
  const [tab, setTab] = useState<Tab>("credentials");
  const [password, setPassword] = useState("");

  return (
    <div className="printable">
      <PageHeader title={a.email} back={href("accounts")} link={<Chip health="ok">Active</Chip>} actions={<>
        <Button icon="lock_reset" onClick={() => open(<SetPassword email={a.email} />)}>New password</Button>
        <Button variant="danger" icon="delete" onClick={() => open(
          <Confirm title="Delete account" label="Delete" message={<>Delete <b>{a.email}</b>? It can no longer sign in. Stored mail stays on disk.</>}
            onYes={async () => {
              await api.deleteAccount(a.email);
              toast("Deleted " + a.email);
              location.hash = href("accounts");
              await onChanged();
            }} />)}>Delete</Button>
      </>} />
      <Metrics items={[
        { label: "Messages", value: a.messages.toLocaleString() },
        { label: "Unread", value: a.unseen.toLocaleString() },
        { label: "Size", value: fmtSize(a.size) },
        { label: "Aliases", value: a.aliases.length ? a.aliases.join(", ") : "none" },
      ]} />
      <Tabs<Tab> value={tab} onChange={setTab} tabs={[["credentials", "Credentials"], ["folders", "Folders"], ["signins", "Sign-ins"]]} />
      {tab === "credentials" && <Credentials email={a.email} password={password} setPassword={setPassword} />}
      {tab === "folders" && <Folders account={a} />}
      {tab === "signins" && <AccountSignIns email={a.email} />}
    </div>
  );
}

function Credentials({ email, password, setPassword }: { email: string; password: string; setPassword: (p: string) => void }) {
  const { ov } = useOverview();
  const copy = useCopy();
  const c = ov?.client;
  if (!c) return <p className="muted">Available once Tailscale Funnel is enabled.</p>;
  return (
    <>
      <div className="toolbar no-print">
        <span className="muted">Add the account in Outlook or the Gmail app as IMAP, with these settings.</span>
        <div className="toolbar-actions">
          <Button icon="content_copy" onClick={() => copy(credentialText(c, email, password || undefined), "Credentials copied")}>Copy all</Button>
          <Button icon="print" onClick={() => window.print()}>Print</Button>
        </div>
      </div>
      <CredentialCards c={c} email={email} password={password || undefined} />
      {!password && (
        <div className="callout info no-print">
          <Icon name="info" />
          <span>Account passwords are stored hashed, so they can't be shown. Generate a new one to fill it in here; the old one stops working in every app.</span>
          <Button small icon="key" onClick={async () => setPassword((await api.setPassword(email, "")).password)}>Generate new password</Button>
        </div>
      )}
    </>
  );
}

function Folders({ account: a }: { account: Account }) {
  return (
    <div className="table-wrap">
      <table className="table">
        <thead><tr><th>Folder</th><th className="num">Messages</th><th className="num">Unread</th><th className="num">Size</th></tr></thead>
        <tbody>
          {(a.mailboxes ?? []).map((m) => (
            <tr key={m.name}>
              <td><span className="with-icon"><Icon name="folder" size={18} />{m.name}</span></td>
              <td className="num">{m.messages.toLocaleString()}</td>
              <td className="num">{m.unseen.toLocaleString()}</td>
              <td className="num">{fmtSize(m.size)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function AccountSignIns({ email }: { email: string }) {
  const { data, error } = useLoad(api.signIns);
  const events = (data?.events ?? []).filter((e) => e.user === email);
  return (
    <div className="table-wrap">
      <table className="table">
        <thead><tr><th className="col-dot" /><th>Time</th><th>App</th><th>From</th><th>Result</th></tr></thead>
        <tbody>
          {error ? <EmptyRow cols={5} icon="error">{error}</EmptyRow>
            : !data ? <EmptyRow cols={5}>Loading…</EmptyRow>
            : !events.length ? <EmptyRow cols={5} icon="key">No sign-ins for this account yet.</EmptyRow>
            : events.map((e, i) => {
              const r = RESULTS[e.result] ?? { label: e.result, health: "none" as const };
              return (
                <tr key={i}>
                  <td className="col-dot"><Dot health={r.health} label={r.label} /></td>
                  <td>{fmtTime(e.at)}</td>
                  <td>{e.proto === "imap" ? "Reading (IMAP)" : "Sending (SMTP)"}{e.client && <span className="muted"> · {e.client}</span>}</td>
                  <td className="mono">{e.ip}</td>
                  <td>{r.label}{e.detail && <span className="muted"> · {e.detail}</span>}</td>
                </tr>
              );
            })}
        </tbody>
      </table>
    </div>
  );
}
