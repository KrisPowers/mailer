import { useEffect, useState, type FormEvent } from "react";
import { Dropdown } from "../../shared/Dropdown";
import { fmtSize, fmtTime, message, plural } from "../../shared/format";
import { Icon, type IconName } from "../../shared/Icon";
import { Avatar, Callout, Card, Copy, MoreMenu, PageHead, PasswordField, Row, SaveSwitch, SubNav, Switch, Tally, TextField, useNotify, Welcome } from "../../shared/ui";
import { api } from "./api";
import { failing, useOverview } from "./overview";
import { CredentialCards, credentialText, PROTO, RESULTS, Steps } from "./parts";
import { accountHref, go } from "./route";
import type { Account, Aliases, Locks, SignIns, Step } from "./types";

// lockedText says which of an account's locks are on, or "" for none.
export function lockedText(l: Locks): string {
  const on = [l.lock_receive && "receiving", l.lock_send && "sending", l.lock_login && "sign-in"].filter(Boolean);
  return on.length ? on.join(", ") + " locked" : "";
}

// A password made or set a moment ago, shown on the account's page until it's
// left: it's stored hashed, so this is the only time anyone sees it.
const fresh = new Map<string, string>();

// Accounts is every mailbox, each row opening the account's own page.
export function Accounts({ accounts }: { accounts: Account[] | null }) {
  const { ov } = useOverview();
  const problems = failing(ov).length;
  const unread = (accounts ?? []).reduce((n, a) => n + a.unseen, 0);
  return (
    <>
      <PageHead title="Accounts" sub={<>
        <span><b>{(accounts?.length ?? 0).toLocaleString()}</b> accounts</span>
        {!!ov?.domains?.length && <span>{ov.domains.join(", ")}</span>}
        {unread > 0 && <span><b>{unread.toLocaleString()}</b> unread</span>}
      </>} actions={<a className="btn primary" href="#/new"><Icon name="add" size={18} />New account</a>} />
      <div className="page-body stack">
        {problems > 0 && (
          <Callout tone="bad" icon="warning" action={<a className="btn small" href="#/status">Status</a>}>
            {plural(problems, "check")} failing. Mail may not be arriving or leaving.
          </Callout>
        )}
        {accounts && accounts.length === 0 && (
          <Welcome icon="group" title="No accounts yet" action={<a className="btn primary" href="#/new"><Icon name="add" size={18} />New account</a>}>
            Each account is an address on your domains, read in the webmail or a mail app.
          </Welcome>
        )}
        {accounts && accounts.length > 0 && (
          <div className="card">
            <ul className="results">
              {accounts.map((a) => {
                const locked = lockedText(a.locks);
                return (
                  <li key={a.email}>
                    <a className="result account-row" href={accountHref(a.email)}>
                      <Avatar name={a.email} />
                      <span className="person">
                        <span className="scan-target">{a.email}</span>
                        <small>{a.aliases.length ? a.aliases.join(", ") : "No aliases"}</small>
                      </span>
                      <span className="chips">
                        <span className={"chip " + (locked ? "warn" : "ok")}>{locked || "Active"}</span>
                        {a.mail_apps && <span className="chip plain">Mail apps</span>}
                      </span>
                      <span className="result-service">{plural(a.messages, "message")}{a.unseen > 0 && <small>{a.unseen.toLocaleString()} unread</small>}</span>
                      <span className="result-meta">{fmtSize(a.size)}</span>
                      <Icon name="chevron_right" className="result-go" />
                    </a>
                  </li>
                );
              })}
            </ul>
          </div>
        )}
      </div>
    </>
  );
}

// NewAccount makes a mailbox, with the password typed or one the server
// makes, and whether its mail apps may sign in from the start.
export function NewAccount({ onAdded }: { onAdded: () => Promise<void> }) {
  const { ov, reload } = useOverview();
  const domains = ov?.domains ?? [];
  const [name, setName] = useState("");
  const [domain, setDomain] = useState("");
  const [password, setPassword] = useState("");
  const [apps, setApps] = useState(false);
  const [busy, setBusy] = useState(false);
  const notify = useNotify();
  const pick = domain || domains[0] || "";
  const ready = name.trim() !== "" && (password === "" || password.length >= 10);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!ready) return;
    setBusy(true);
    try {
      const local = name.trim();
      const r = await api.createAccount(local.includes("@") ? local : local + "@" + pick, password, apps);
      fresh.set(r.email, r.password);
      await Promise.all([onAdded(), reload()]);
      notify("Created " + r.email);
      go(accountHref(r.email));
    } catch (err) {
      notify(message(err), "bad");
      setBusy(false);
    }
  };

  return (
    <form onSubmit={submit}>
      <PageHead back="#/" title="New account" sub="An address on one of your domains. It reads mail in the webmail, and in mail apps once those are on." />
      <div className="page-body stack">
        {!domains.length && (
          <Callout tone="warn" icon="cloud" action={<a className="btn small" href="#/setup">Setup</a>}>Connect Cloudflare first, so your domains are known.</Callout>
        )}
        <div className="card form-card">
          <div className="composer-row">
            <TextField className="grow" label="Name" autoFocus autoComplete="off" value={name}
              onChange={(v) => setName(v.replace(/\s/g, "").toLowerCase())} hint="Before the @, such as kris or hello" />
            <span className="composer-dot">@</span>
            <div className="tf static domain-pick">
              <div className="tf-box">
                {domains.length > 0
                  ? <Dropdown label="Domain" value={pick} onChange={setDomain} options={domains.map((d) => ({ value: d, label: d }))} />
                  : <span className="dropdown placeholder">No domains yet</span>}
                <span className="tf-label">Domain</span>
              </div>
            </div>
          </div>
          <PasswordField label="Password" generate value={password} onChange={setPassword}
            hint={password && password.length < 10 ? `${10 - password.length} more characters.` : "At least 10 characters. Leave it empty and one is made for you."} />
          <Row title="Mail apps" sub="Let Outlook, Thunderbird and phone mail apps sign in to this account. The webmail works either way.">
            <Switch label="Mail apps" on={apps} onChange={setApps} />
          </Row>
        </div>
        <div className="form-foot">
          <a className="btn text" href="#/">Cancel</a>
          <button className="btn primary" disabled={busy || !ready || !pick}>{busy && <span className="spinner small on-accent" />}Create account</button>
        </div>
      </div>
    </form>
  );
}

const PAGES: [string, string, IconName][] = [
  ["overview", "Overview", "person"],
  ["apps", "Mail apps", "smartphone"],
  ["aliases", "Aliases", "alternate_email"],
  ["folders", "Folders", "folder"],
  ["signins", "Sign-ins", "key"],
];

// AccountPage is one account: its access and password, the settings its mail
// apps need, its other addresses, its folders and its sign-ins.
export function AccountPage({ account: a, page, onChanged }: { account: Account; page: string; onChanged: () => Promise<void> }) {
  const { reload } = useOverview();
  const notify = useNotify();
  const current = PAGES.some(([k]) => k === page) ? page : "overview";
  const [password, setPassword] = useState(fresh.get(a.email) ?? "");
  const locked = lockedText(a.locks);
  useEffect(() => () => { fresh.delete(a.email); }, [a.email]);

  const newPassword = async (pw: string) => {
    const r = await api.setPassword(a.email, pw);
    setPassword(r.password);
    notify("Password set. The old one stopped working everywhere.");
  };
  const remove = async () => {
    try {
      await api.deleteAccount(a.email);
      notify("Deleted " + a.email);
      await Promise.all([onChanged(), reload()]);
      go("#/");
    } catch (e) {
      notify(message(e), "bad");
    }
  };

  return (
    <>
      <PageHead lead={<Avatar name={a.email} big />} title={a.email} sub={<>
        <span className={"chip " + (locked ? "warn" : "ok")}>{locked || "Active"}</span>
        <span className={"chip " + (a.mail_apps ? "plain" : "")}>{a.mail_apps ? "Mail apps on" : "Webmail only"}</span>
        {a.aliases.length > 0 && <span>{plural(a.aliases.length, "alias", "aliases")}</span>}
      </>} actions={
        <MoreMenu label="Account actions" actions={[
          { label: "Copy address", icon: "content_copy", onClick: () => { navigator.clipboard?.writeText(a.email); notify("Address copied"); } },
          { label: "Make a new password", icon: "key", onClick: () => newPassword("").catch((e) => notify(message(e), "bad")) },
          { label: "Delete account", icon: "delete", danger: true, confirm: "Delete, mail stays on disk", onClick: remove },
        ]} />
      } />
      <SubNav back="#/" backLabel="Accounts" base={accountHref(a.email)} items={PAGES} current={current} />
      <div className="page-body stack" key={current}>
        {password && (
          <Callout tone="warn" icon="key" action={<button type="button" className="btn small" onClick={() => setPassword("")}>Done</button>}>
            Password for <b>{a.email}</b>: <Copy text={password} />. This is the only time it's shown.
          </Callout>
        )}
        {current === "overview" && <Overview account={a} onChanged={onChanged} onPassword={newPassword} />}
        {current === "apps" && <MailApps account={a} password={password} onChanged={onChanged} />}
        {current === "aliases" && <AliasesPage email={a.email} onChanged={onChanged} />}
        {current === "folders" && <Folders account={a} />}
        {current === "signins" && <SignInsPage email={a.email} />}
      </div>
    </>
  );
}

const ACCESS: { key: keyof Locks; title: string; sub: string }[] = [
  { key: "lock_receive", title: "Receive mail", sub: "Off refuses mail to this address and its aliases." },
  { key: "lock_send", title: "Send mail", sub: "Off keeps reading working but refuses everything it sends." },
  { key: "lock_login", title: "Sign in", sub: "Off keeps mail arriving but no one can sign in, in the webmail or any app." },
];

function Overview({ account: a, onChanged, onPassword }: { account: Account; onChanged: () => Promise<void>; onPassword: (pw: string) => Promise<void> }) {
  const [editing, setEditing] = useState(false);
  const [pw, setPw] = useState("");
  const [busy, setBusy] = useState(false);
  const notify = useNotify();
  const setLock = async (key: keyof Locks, allowed: boolean) => {
    await api.setLocks(a.email, { ...a.locks, [key]: !allowed });
    await onChanged();
  };
  return (
    <>
      <Tally items={[
        { label: "Messages", value: a.messages.toLocaleString(), tone: "plain" },
        { label: "Unread", value: a.unseen.toLocaleString(), tone: a.unseen ? "plain" : undefined },
        { label: "Stored", value: fmtSize(a.size) },
        { label: "Folders", value: (a.mailboxes?.length ?? 0).toLocaleString() },
      ]} />
      <Card title="Access" sub="Each switch stops one thing and leaves the others working.">
        {ACCESS.map((c) => (
          <Row key={c.key} title={c.title} sub={c.sub}>
            <SaveSwitch label={c.title} on={!a.locks[c.key]} onSave={(v) => setLock(c.key, v)} />
          </Row>
        ))}
        <Row title="Mail apps" sub="Lets Outlook, Thunderbird and phone mail apps sign in over IMAP and SMTP. The webmail works either way.">
          <SaveSwitch label="Mail apps" on={a.mail_apps} onSave={async (v) => { await api.setMailApps(a.email, v); await onChanged(); }} />
        </Row>
      </Card>
      <Card title="Password" sub="Stored hashed, so it can't be shown again. Setting a new one stops the old one at once, everywhere.">
        {editing ? (
          <form className="card-pad" onSubmit={async (e) => {
            e.preventDefault();
            setBusy(true);
            try {
              await onPassword(pw);
              setPw("");
              setEditing(false);
            } catch (err) {
              notify(message(err), "bad");
            } finally {
              setBusy(false);
            }
          }}>
            <PasswordField label="New password" generate autoFocus value={pw} onChange={setPw} hint="At least 10 characters. Leave it empty and one is made for you." />
            <div className="edit-actions">
              <button type="button" className="btn text" onClick={() => setEditing(false)}>Cancel</button>
              <button className="btn primary" disabled={busy || (pw !== "" && pw.length < 10)}>Set password</button>
            </div>
          </form>
        ) : (
          <Row title="Password" sub="Set">
            <button type="button" className="btn" onClick={() => setEditing(true)}><Icon name="lock_reset" size={18} />New password</button>
          </Row>
        )}
      </Card>
    </>
  );
}

function MailApps({ account: a, password, onChanged }: { account: Account; password: string; onChanged: () => Promise<void> }) {
  const { ov } = useOverview();
  const notify = useNotify();
  const c = ov?.client;
  return (
    <>
      {!a.mail_apps && (
        <Callout tone="warn" icon="block" action={
          <button type="button" className="btn small" onClick={() => api.setMailApps(a.email, true).then(onChanged).catch((e) => notify(message(e), "bad"))}>Turn on</button>
        }>Mail apps are off for this account, so these settings are refused until they're turned on.</Callout>
      )}
      {!c ? (
        <Callout icon="lan" action={<a className="btn small" href="#/setup">Setup</a>}>The settings show once Tailscale Funnel is on.</Callout>
      ) : (
        <>
          <CredentialCards c={c} email={a.email} password={password || undefined} />
          <div className="form-foot no-print">
            <button type="button" className="btn" onClick={() => { navigator.clipboard?.writeText(credentialText(c, a.email, password || undefined)); notify("Settings copied"); }}>
              <Icon name="content_copy" size={18} />Copy all
            </button>
            <button type="button" className="btn" onClick={() => print()}><Icon name="print" size={18} />Print</button>
          </div>
          <p className="lede">Add it in Outlook, Thunderbird or a phone's mail app as an IMAP account. Most find these by themselves from the address and password.</p>
        </>
      )}
    </>
  );
}

// AliasesPage holds the other addresses on this account's domain that deliver
// here, and whether the domain's unknown addresses land here too.
function AliasesPage({ email, onChanged }: { email: string; onChanged: () => Promise<void> }) {
  const domain = email.split("@")[1] ?? "";
  const [map, setMap] = useState<Aliases | null>(null);
  const [local, setLocal] = useState("");
  const notify = useNotify();
  const load = () => api.aliases().then(setMap).catch((e) => notify(message(e), "bad"));
  useEffect(() => { load(); }, []);

  const all = map ?? {};
  const mine = Object.keys(all).filter((k) => !k.startsWith("@") && all[k] === email).sort();
  const catchAll = all["@" + domain] ?? "";
  const save = async (next: Aliases, done: string) => {
    await api.saveAliases(next);
    notify(done);
    await Promise.all([load(), onChanged()]);
  };
  const add = async (e: FormEvent) => {
    e.preventDefault();
    const alias = local.trim().toLowerCase() + "@" + domain;
    if (all[alias] && all[alias] !== email) return notify(`${alias} already delivers to ${all[alias]}`, "bad");
    try {
      await save({ ...all, [alias]: email }, "Added " + alias);
      setLocal("");
    } catch (err) {
      notify(message(err), "bad");
    }
  };

  return (
    <>
      <Card title="Aliases" sub="Other addresses whose mail lands in this account. It can send as them too.">
        <form className="card-pad composer-row" onSubmit={add}>
          <TextField className="grow" label="New alias" value={local} onChange={(v) => setLocal(v.replace(/[\s@]/g, ""))}
            hint={<>Mail to <code>{(local.trim() || "name").toLowerCase()}@{domain}</code> lands here</>} />
          <button className="btn primary composer-go" disabled={!local.trim()}><Icon name="add" size={18} />Add</button>
        </form>
        {mine.length ? (
          <ul className="item-list card-pad">
            {mine.map((k) => (
              <li key={k} className="item-row">
                <Icon name="alternate_email" size={18} className="item-icon" />
                <span className="person"><span className="scan-target">{k}</span></span>
                <button type="button" className="icon-btn small danger" aria-label={`Remove ${k}`} title="Remove"
                  onClick={() => { const next = { ...all }; delete next[k]; save(next, "Removed " + k).catch((e) => notify(message(e), "bad")); }}>
                  <Icon name="close" size={18} />
                </button>
              </li>
            ))}
          </ul>
        ) : <p className="empty-note card-pad">{map ? "No aliases" : "Loading"}</p>}
      </Card>
      <Card title="Catch-all">
        <Row title={`Any unknown address @${domain}`} sub={catchAll && catchAll !== email ? `Now goes to ${catchAll}` : catchAll === email ? "Lands here" : "Refused"}>
          <SaveSwitch label="Catch-all" on={catchAll === email} onSave={(on) => {
            const next = { ...all };
            if (on) next["@" + domain] = email;
            else delete next["@" + domain];
            return save(next, on ? `Unknown addresses @${domain} land here` : `Catch-all for @${domain} off`);
          }} />
        </Row>
      </Card>
    </>
  );
}

function Folders({ account: a }: { account: Account }) {
  return (
    <div className="card">
      <div className="table-wrap">
        <table className="table">
          <thead><tr><th className="grow">Folder</th><th>Messages</th><th>Unread</th><th>Size</th></tr></thead>
          <tbody>
            {(a.mailboxes ?? []).map((m) => (
              <tr key={m.name}>
                <td><span className="log-event"><Icon name="folder" size={18} />{m.name}</span></td>
                <td>{m.messages.toLocaleString()}</td>
                <td>{m.unseen.toLocaleString()}</td>
                <td>{fmtSize(m.size)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function SignInsPage({ email }: { email: string }) {
  const [data, setData] = useState<SignIns | null>(null);
  const [password, setPassword] = useState("");
  const [steps, setSteps] = useState<Step[] | null>(null);
  const [busy, setBusy] = useState(false);
  const notify = useNotify();
  const load = () => api.signIns().then(setData).catch((e) => notify(message(e), "bad"));
  useEffect(() => { load(); }, []);
  const events = (data?.events ?? []).filter((e) => e.user === email);

  return (
    <>
      <Card title="Test sign-in" sub="Signs in to IMAP and SMTP the way a mail app would, through Funnel, and says where it stops.">
        <form className="card-pad composer-row" onSubmit={async (e) => {
          e.preventDefault();
          setBusy(true);
          setSteps(null);
          try {
            setSteps((await api.testSignIn(email, password)).steps);
          } catch (err) {
            notify(message(err), "bad");
          } finally {
            setBusy(false);
          }
        }}>
          <PasswordField className="grow" label="Account password" autoComplete="off" value={password} onChange={setPassword} />
          <button className="btn primary composer-go" disabled={busy || !password}>{busy && <span className="spinner small on-accent" />}Test</button>
        </form>
        {steps && <div className="card-pad"><Steps steps={steps} /></div>}
      </Card>
      <Card title="Sign-ins" sub="The webmail and mail apps, newest first.">
        {events.length ? (
          <div className="table-wrap">
            <table className="table log-table">
              <thead><tr><th>Time</th><th className="grow">Result</th><th>App</th><th>Address</th><th /></tr></thead>
              <tbody>
                {events.map((e, i) => {
                  const r = RESULTS[e.result] ?? { label: e.result, tone: "", fix: "" };
                  return (
                    <tr key={i} title={[e.detail, r.fix, e.commands?.length ? "Commands: " + e.commands.join(", ") : ""].filter(Boolean).join("\n")}>
                      <td className="log-time">{fmtTime(e.at)}</td>
                      <td><span className="log-event"><span className={"dot " + r.tone} />{r.label}{e.detail && <small className="muted"> {e.detail}</small>}</span></td>
                      <td>{PROTO[e.proto] ?? e.proto}{e.client && <span className="muted"> {e.client}</span>}</td>
                      <td className="mono muted">{e.ip}</td>
                      <td>{e.result === "blocked" && (
                        <button type="button" className="btn small" onClick={() => api.unblock(e.ip).then(() => notify("Unblocked " + e.ip)).catch((err) => notify(message(err), "bad"))}>Unblock</button>
                      )}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : <p className="empty-note card-pad">{data ? "No sign-ins yet" : "Loading"}</p>}
      </Card>
    </>
  );
}
