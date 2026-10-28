import { useState } from "react";
import { api } from "../api";
import { SetPassword } from "../flows/Accounts";
import { CredentialCards, credentialText } from "../flows/MailSettings";
import { fmtSize, fmtTime } from "../format";
import { useOverview } from "../overview";
import { href } from "../router";
import type { Account } from "../types";
import { Button, IconButton } from "../ui/Button";
import { TextInput } from "../ui/Field";
import { Select } from "../ui/Select";
import { Toggle } from "../ui/Toggle";
import { Icon } from "../ui/Icon";
import { Confirm, useModal } from "../ui/Modal";
import { Chip, Dot, EmptyRow, Metrics, PageHeader, Section, Tabs } from "../ui/Page";
import { useCopy, useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";
import { RESULTS } from "./SignIns";

type Tab = "credentials" | "aliases" | "folders" | "signins";

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
      <Tabs<Tab> value={tab} onChange={setTab} tabs={[["credentials", "Credentials"], ["aliases", "Aliases"], ["folders", "Folders"], ["signins", "Sign-ins"]]} />
      {tab === "credentials" && <Credentials email={a.email} password={password} setPassword={setPassword} />}
      {tab === "aliases" && <AliasesTab email={a.email} onChanged={onChanged} />}
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

// AliasesTab holds the other addresses that deliver to this account, and
// which domains send mail for unknown addresses here (catch-all).
function AliasesTab({ email, onChanged }: { email: string; onChanged: () => Promise<void> }) {
  const { ov } = useOverview();
  const domains = ov?.domains ?? [];
  const { data, error, reload } = useLoad(api.aliases);
  const toast = useToast();
  const copy = useCopy();
  const { open } = useModal();
  const [local, setLocal] = useState("");
  const [domain, setDomain] = useState(email.split("@")[1] ?? domains[0] ?? "");

  const map = data ?? {};
  const mine = Object.keys(map).filter((k) => !k.startsWith("@") && map[k] === email).sort();
  const save = async (next: Record<string, string>, msg: string) => {
    await api.saveAliases(next);
    toast(msg);
    await Promise.all([reload(), onChanged()]);
  };
  const add = async () => {
    const alias = local.trim().toLowerCase() + "@" + domain;
    if (map[alias] && map[alias] !== email) throw new Error(alias + " already delivers to " + map[alias]);
    await save({ ...map, [alias]: email }, "Added " + alias);
    setLocal("");
  };

  return (
    <>
      <Section title="Aliases">
        <div className="inline-form">
          <TextInput placeholder="hello" aria-label="New alias" value={local} onChange={setLocal}
            onKeyDown={(e) => e.key === "Enter" && local.trim() && add().catch((err) => toast((err as Error).message, "bad"))} />
          {domains.length > 1
            ? <Select value={domain} options={domains} onChange={setDomain} label="Alias domain" />
            : <span className="muted at-domain">@{domain}</span>}
          <Button variant="primary" icon="add" disabled={!local.trim()} onClick={add}>Add alias</Button>
        </div>
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th className="col-dot" /><th className="grow">Alias</th><th className="col-actions">Actions</th></tr></thead>
            <tbody>
              {error ? <EmptyRow cols={3} icon="error">{error}</EmptyRow>
                : !data ? <EmptyRow cols={3}>Loading…</EmptyRow>
                : !mine.length ? <EmptyRow cols={3} icon="alternate_email">No aliases. Mail to an alias lands in this account.</EmptyRow>
                : mine.map((k) => (
                  <tr key={k}>
                    <td className="col-dot"><Dot health="ok" label="Delivering" /></td>
                    <td>{k}</td>
                    <td className="col-actions">
                      <span className="row-actions">
                        <IconButton icon="content_copy" label="Copy alias" onClick={() => copy(k, "Alias copied")} />
                        <span className="divider" />
                        <IconButton icon="delete" label="Delete alias" tone="danger" onClick={() => open(
                          <Confirm title="Delete alias" label="Delete" message={<>Stop accepting mail for <b>{k}</b>?</>}
                            onYes={async () => {
                              const next = { ...map };
                              delete next[k];
                              await save(next, "Deleted " + k);
                            }} />)} />
                      </span>
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
        <p className="muted small">Plus-addressing always works: <code>{email.replace("@", "+anything@")}</code> lands here too.</p>
      </Section>
      <Section title="Catch-all">
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th className="col-dot" /><th>Domain</th><th className="grow">Unknown addresses go to</th><th className="col-actions">Here</th></tr></thead>
            <tbody>
              {domains.map((d) => {
                const target = map["@" + d] ?? "";
                return (
                  <tr key={d}>
                    <td className="col-dot"><Dot health={target ? "ok" : "none"} label={target ? "Catch-all on" : "Catch-all off"} /></td>
                    <td>@{d}</td>
                    <td className={target === email ? "" : "muted"}>{target === email ? "this account" : target || "nobody: unknown addresses are refused"}</td>
                    <td className="col-actions">
                      <Toggle checked={target === email} onChange={(on) => {
                        const next = { ...map };
                        if (on) next["@" + d] = email; else delete next["@" + d];
                        save(next, on ? "Catch-all for @" + d + " delivers here" : "Catch-all for @" + d + " turned off")
                          .catch((err) => toast((err as Error).message, "bad"));
                      }} />
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </Section>
    </>
  );
}

function Folders({ account: a }: { account: Account }) {
  return (
    <div className="table-wrap">
      <table className="table">
        <thead><tr><th>Folder</th><th className="num">Messages</th><th className="num">Unread</th><th className="num grow">Size</th></tr></thead>
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
        <thead><tr><th className="col-dot" /><th>Time</th><th>App</th><th>From</th><th className="grow">Result</th></tr></thead>
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
