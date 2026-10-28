import { useState } from "react";
import { api } from "../api";
import { SetPassword } from "../flows/Accounts";
import { CredentialCards, credentialText } from "../flows/MailSettings";
import { fmtSize, fmtTime } from "../format";
import { useOverview, type Health } from "../overview";
import { href } from "../router";
import type { Account, Locks, SignInEvent, Step } from "../types";
import { Button, IconButton } from "../ui/Button";
import { PasswordInput } from "../ui/Field";
import { PopoverButton } from "../ui/Floating";
import { Icon } from "../ui/Icon";
import { Confirm, useModal } from "../ui/Modal";
import { Chip, Dot, EmptyRow, Metrics, PageHeader, Section, Steps, Tabs } from "../ui/Page";
import { Toggle } from "../ui/Toggle";
import { useCopy, useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

type Tab = "credentials" | "controls" | "aliases" | "folders" | "signins";

// lockedText says which of an account's locks are on, or "" for none.
export function lockedText(l: Locks): string {
  const on = [l.lock_receive && "receiving", l.lock_send && "sending", l.lock_login && "sign-in"].filter(Boolean);
  return on.length ? on.join(", ") + " locked" : "";
}

// AccountPage is one account, like a container's page in Docker Desktop.
export function AccountPage({ account: a, onChanged }: { account: Account; onChanged: () => Promise<void> }) {
  const { open } = useModal();
  const toast = useToast();
  const [tab, setTab] = useState<Tab>("credentials");
  const [password, setPassword] = useState("");
  const locked = lockedText(a.locks);

  return (
    <div className="printable">
      <PageHeader title={a.email} back={href("accounts")} link={<Chip health={locked ? "warn" : "ok"}>{locked || "Active"}</Chip>} actions={<>
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
      ]} />
      <Tabs<Tab> value={tab} onChange={setTab} tabs={[
        ["credentials", "Credentials"], ["controls", "Controls"], ["aliases", "Aliases"], ["folders", "Folders"], ["signins", "Sign-ins"],
      ]} />
      {tab === "credentials" && <Credentials email={a.email} password={password} setPassword={setPassword} />}
      {tab === "controls" && <Controls account={a} onChanged={onChanged} />}
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
        <div className="toolbar-actions">
          {!password && <Button icon="key" onClick={async () => setPassword((await api.setPassword(email, "")).password)}>Generate new password</Button>}
          <Button icon="content_copy" onClick={() => copy(credentialText(c, email, password || undefined), "Credentials copied")}>Copy all</Button>
          <Button icon="print" onClick={() => window.print()}>Print</Button>
        </div>
      </div>
      <CredentialCards c={c} email={email} password={password || undefined} />
    </>
  );
}

const CONTROLS: { key: keyof Locks; title: string; on: string }[] = [
  { key: "lock_receive", title: "Receive mail", on: "Mail to this address and its aliases is refused" },
  { key: "lock_send", title: "Send mail", on: "Mail apps can sign in and read, but sending is refused" },
  { key: "lock_login", title: "Sign in", on: "Mail keeps arriving, but no app can sign in" },
];

// Controls switches each part of an account on or off on its own.
function Controls({ account: a, onChanged }: { account: Account; onChanged: () => Promise<void> }) {
  const toast = useToast();
  const set = async (key: keyof Locks, allowed: boolean) => {
    await api.setLocks(a.email, { ...a.locks, [key]: !allowed });
    toast(CONTROLS.find((c) => c.key === key)!.title + (allowed ? " allowed" : " locked"));
    await onChanged();
  };
  return (
    <div className="table-wrap">
      <table className="table">
        <thead><tr><th className="col-dot" /><th>Control</th><th className="grow">When locked</th><th className="col-actions">Allowed</th></tr></thead>
        <tbody>
          {CONTROLS.map((c) => {
            const locked = !!a.locks[c.key];
            return (
              <tr key={c.key}>
                <td className="col-dot"><Dot health={locked ? "warn" : "ok"} label={locked ? "Locked" : "Allowed"} /></td>
                <td className="strong">{c.title}</td>
                <td className="muted wrap">{c.on}</td>
                <td className="col-actions">
                  <Toggle checked={!locked} onChange={(v) => set(c.key, v).catch((err) => toast((err as Error).message, "bad"))} />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

// AliasesTab holds the other addresses on this account's domain that
// deliver here, and whether its domain's unknown addresses land here too.
function AliasesTab({ email, onChanged }: { email: string; onChanged: () => Promise<void> }) {
  const domain = email.split("@")[1] ?? "";
  const { data, error, reload } = useLoad(api.aliases);
  const toast = useToast();
  const copy = useCopy();
  const { open } = useModal();
  const [local, setLocal] = useState("");

  const map = data ?? {};
  const mine = Object.keys(map).filter((k) => !k.startsWith("@") && map[k] === email).sort();
  const catchAll = map["@" + domain] ?? "";
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
          <div className="input address">
            <input type="text" placeholder="hello" aria-label="New alias" spellCheck={false} value={local} onChange={(e) => setLocal(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && local.trim() && add().catch((err) => toast((err as Error).message, "bad"))} />
            <span className="at">@</span><span className="domain">{domain}</span>
          </div>
          <Button variant="primary" icon="add" disabled={!local.trim()} onClick={add}>Add alias</Button>
        </div>
        <div className="table-wrap">
          <table className="table">
            <thead><tr><th className="col-dot" /><th className="grow">Alias</th><th className="col-actions">Actions</th></tr></thead>
            <tbody>
              {error ? <EmptyRow cols={3} icon="error">{error}</EmptyRow>
                : !data ? <EmptyRow cols={3}>Loading…</EmptyRow>
                : !mine.length ? <EmptyRow cols={3} icon="alternate_email">No aliases</EmptyRow>
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
      </Section>
      <Section title="Catch-all">
        <div className="table-wrap">
          <table className="table">
            <tbody>
              <tr>
                <td className="col-dot"><Dot health={catchAll === email ? "ok" : "none"} label={catchAll === email ? "On" : "Off"} /></td>
                <td className="grow">
                  Any unknown address @{domain}
                  {catchAll && catchAll !== email && <span className="muted"> · now goes to {catchAll}</span>}
                </td>
                <td className="col-actions">
                  <Toggle checked={catchAll === email} onChange={(on) => {
                    const next = { ...map };
                    if (on) next["@" + domain] = email; else delete next["@" + domain];
                    save(next, on ? "Catch-all for @" + domain + " delivers here" : "Catch-all for @" + domain + " off")
                      .catch((err) => toast((err as Error).message, "bad"));
                  }} />
                </td>
              </tr>
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

// What each sign-in result means, and what to change in the mail app.
const RESULTS: Record<string, { label: string; health: Health; fix: string }> = {
  ok: { label: "Signed in", health: "ok", fix: "" },
  bad_password: { label: "Wrong password", health: "bad", fix: "Re-enter the password in the app, or set a new one." },
  unknown_user: { label: "No such account", health: "bad", fix: "The username must be the full email address." },
  blocked: { label: "Blocked", health: "bad", fix: "Too many failures from this address. Unblock it on the Dashboard once the password is fixed." },
  locked: { label: "Sign-in locked", health: "warn", fix: "Allow sign-in under Controls." },
  needs_tls: { label: "Not encrypted", health: "bad", fix: "Set the app's security to SSL/TLS." },
  unsupported: { label: "Method not offered", health: "warn", fix: "Choose normal password sign-in in the app." },
  malformed: { label: "Unreadable sign-in", health: "warn", fix: "Remove the account from the app and add it again." },
  no_sign_in: { label: "Left early", health: "warn", fix: "The app connected but left before signing in." },
};

function AccountSignIns({ email }: { email: string }) {
  const { data, error } = useLoad(api.signIns);
  const [password, setPassword] = useState("");
  const [steps, setSteps] = useState<Step[] | null>(null);
  const events = (data?.events ?? []).filter((e) => e.user === email);
  return (
    <>
      <div className="inline-form test-form">
        <PasswordInput value={password} onChange={setPassword} placeholder="Account password" />
        <Button variant="primary" icon="login" disabled={!password} onClick={async () => {
          setSteps(null);
          setSteps((await api.testSignIn(email, password)).steps);
        }}>Test sign-in</Button>
      </div>
      {steps && <Steps steps={steps} />}
      <div className="table-wrap">
        <table className="table">
          <thead><tr><th className="col-dot" /><th>Time</th><th>App</th><th>From</th><th className="grow">Result</th><th className="col-actions">Actions</th></tr></thead>
          <tbody>
            {error ? <EmptyRow cols={6} icon="error">{error}</EmptyRow>
              : !data ? <EmptyRow cols={6}>Loading…</EmptyRow>
              : !events.length ? <EmptyRow cols={6} icon="key">No sign-ins yet</EmptyRow>
              : events.map((e, i) => <SignInRow key={i} e={e} />)}
          </tbody>
        </table>
      </div>
    </>
  );
}

function SignInRow({ e }: { e: SignInEvent }) {
  const r = RESULTS[e.result] ?? { label: e.result, health: "none" as Health, fix: "" };
  return (
    <tr>
      <td className="col-dot"><Dot health={r.health} label={r.label} /></td>
      <td>{fmtTime(e.at)}</td>
      <td>{e.proto === "imap" ? "IMAP" : "SMTP"}{e.client && <span className="muted"> · {e.client}</span>}</td>
      <td className="mono">{e.ip}</td>
      <td>{r.label}</td>
      <td className="col-actions">
        {(!!e.detail || !!r.fix) && (
          <PopoverButton label="Details" content={
            <div className="popover-body">
              <h3>{r.label}</h3>
              {e.detail && <p>{e.detail}</p>}
              {r.fix && <p>{r.fix}</p>}
              {!!e.commands?.length && <p className="muted">Commands: <code>{e.commands.join(", ")}</code></p>}
            </div>
          }><Icon name="info" /></PopoverButton>
        )}
      </td>
    </tr>
  );
}
