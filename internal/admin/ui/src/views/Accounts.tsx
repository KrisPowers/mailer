import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { AsyncButton } from "../components/AsyncButton";
import { Badge } from "../components/Badge";
import { Confirm } from "../components/Confirm";
import { CopyButton } from "../components/CopyButton";
import { DomainField } from "../components/DomainField";
import { PasswordField } from "../components/PasswordField";
import { MoreMenu, type MenuItem } from "../components/Floating";
import { Icon } from "../components/Icon";
import { EmptyRow, PageHead, Search } from "../components/Layout";
import { CloseButton, Modal, useModal } from "../components/Modal";
import { useCopy, useToast } from "../components/Toasts";
import { fmtSize, plural } from "../format";
import { useOverview } from "../overview";
import { PasswordModal } from "../setup/PasswordModal";
import type { Account } from "../types";
import { useLoad } from "../useLoad";

export function Accounts() {
  const { ov, reload: reloadOverview } = useOverview();
  const { data, error, reload } = useLoad(api.accounts);
  const { open } = useModal();
  const copy = useCopy();
  const toast = useToast();
  const [q, setQ] = useState("");

  const accounts = data ?? [];
  const query = q.toLowerCase();
  const list = accounts.filter((a) => a.email.includes(query) || a.aliases.some((x) => x.includes(query)));

  const refresh = async () => {
    await Promise.all([reload(), reloadOverview()]);
  };

  const remove = (a: Account) => open(
    <Confirm title="Delete account" label="Delete" danger
      message={`Delete ${a.email}? It can no longer sign in. Stored mail stays on disk.`}
      onYes={async () => {
        await api.deleteAccount(a.email);
        toast("Deleted " + a.email);
        await refresh();
      }} />);

  const menu = (a: Account) => (): MenuItem[] => [
    { label: "Credentials", icon: "key", onClick: () => open(<AccountDetails account={a} />) },
    { label: "Folders", icon: "folder", onClick: () => open(<AccountDetails account={a} tab="folders" />) },
    { label: "Copy address", icon: "copy", onClick: () => copy(a.email, "Address copied") },
    "-",
    { label: "Set password", icon: "edit", onClick: () => open(<SetPassword email={a.email} />) },
    { label: "Delete account", icon: "trash", danger: true, onClick: () => remove(a) },
  ];

  return (
    <>
      <PageHead title="Accounts" sub={data ? plural(accounts.length, "mailbox", "mailboxes") : ""} />
      <div className="toolbar">
        <Search value={q} onChange={setQ} placeholder="Search" />
        <div className="actions">
          <button className="btn primary" disabled={!ov?.domain} onClick={() => open(<NewAccount domains={ov?.domains ?? []} onCreated={refresh} />)}>
            <Icon name="plus" />New account
          </button>
        </div>
      </div>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th className="dotcol" /><th>Name</th><th>Aliases</th><th className="num">Messages</th>
              <th className="num">Unread</th><th className="num">Size</th><th>Actions</th>
            </tr>
          </thead>
          <tbody>
            {error ? <EmptyRow cols={7}>{error}</EmptyRow>
              : !data ? <EmptyRow cols={7}>Loading…</EmptyRow>
              : !list.length ? <EmptyRow cols={7}>{accounts.length ? "No accounts match." : "No accounts yet. Create one with New account."}</EmptyRow>
              : list.map((a) => (
                <tr key={a.email}>
                  <td className="dotcol"><span className="dot ok" title="Active" /></td>
                  <td><button className="name-link" onClick={() => open(<AccountDetails account={a} />)}>{a.email}</button></td>
                  <td>{a.aliases.length ? <Badge kind="plain">{plural(a.aliases.length, "alias", "aliases")}</Badge> : <span className="muted">none</span>}</td>
                  <td className="num">{a.messages.toLocaleString()}</td>
                  <td className="num">{a.unseen ? <b>{a.unseen.toLocaleString()}</b> : "0"}</td>
                  <td className="num">{fmtSize(a.size)}</td>
                  <td className="act">
                    <span className="row-actions">
                      <button className="icon-btn accent" title="Credentials" onClick={() => open(<AccountDetails account={a} />)}><Icon name="key" /></button>
                      <MoreMenu items={menu(a)} />
                      <span className="sep" />
                      <button className="icon-btn danger" title="Delete account" onClick={() => remove(a)}><Icon name="trash" /></button>
                    </span>
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>
      {data && <div className="showing">Showing {plural(list.length, "item")}</div>}
    </>
  );
}

// AccountDetails is an account's own page, after a Docker container's:
// Credentials lists everything a mail app needs, Folders what it holds.
export function AccountDetails({ account: a, tab: first = "credentials" }: { account: Account; tab?: "credentials" | "folders" }) {
  const [tab, setTab] = useState(first);
  return (
    <Modal title={a.email} wide footer={<CloseButton />}>
      <div className="tabs" role="tablist">
        <button role="tab" aria-selected={tab === "credentials"} className={"tab" + (tab === "credentials" ? " active" : "")} onClick={() => setTab("credentials")}>Credentials</button>
        <button role="tab" aria-selected={tab === "folders"} className={"tab" + (tab === "folders" ? " active" : "")} onClick={() => setTab("folders")}>Folders</button>
      </div>
      {tab === "credentials" ? <Credentials email={a.email} /> : <Folders account={a} />}
    </Modal>
  );
}

// Credentials prints the IMAP and SMTP settings for one account, the
// relay token included. Stored passwords can't be read back, so a new one
// can be made here and is shown in place.
function Credentials({ email }: { email: string }) {
  const { ov } = useOverview();
  const copy = useCopy();
  const [password, setPassword] = useState("");
  const c = ov?.client;
  if (!c) return <p className="muted">Available once Tailscale Funnel is enabled.</p>;

  const relay = !!c.smtp_user;
  const accountPw = password || "the account's password";
  const rows = {
    imap: [["Server", c.server], ["Port", String(c.imap_port)], ["Security", "SSL/TLS"], ["Username", email], ["Password", accountPw]],
    smtp: [["Server", c.smtp_server], ["Port", String(c.smtp_port)], ["Security", "SSL/TLS"],
      ["Username", relay ? c.smtp_user! : email], ["Password", relay ? c.smtp_password ?? "" : accountPw]],
  };
  const text = [`Account: ${email}`, "", "Incoming mail (IMAP)", ...rows.imap.map(([k, v]) => `  ${k}: ${v}`),
    "", "Outgoing mail (SMTP)", ...rows.smtp.map(([k, v]) => `  ${k}: ${v}`)].join("\n");

  const section = (title: string, list: string[][]) => (
    <div className="cred">
      <h3><Icon name={title.startsWith("Incoming") ? "inbox" : "send"} />{title}</h3>
      <dl className="kv">
        {list.map(([k, v]) => {
          const known = !(k === "Password" && v === accountPw && !password);
          return [
            <dt key={k + "t"}>{k}</dt>,
            <dd key={k + "d"} className={known ? "mono" : "muted"}>{v}</dd>,
            known ? <CopyButton key={k + "c"} text={v} label={k + " copied"} /> : <span key={k + "c"} />,
          ];
        })}
      </dl>
    </div>
  );

  return (
    <>
      <div className="toolbar">
        <span className="muted">Enter these in Outlook or the Gmail app as an IMAP account.</span>
        <div className="actions cred-actions">
          <button className="btn sm" onClick={() => copy(text, "Credentials copied")}><Icon name="copy" />Copy all</button>
          <button className="btn sm" onClick={() => window.print()}><Icon name="printer" />Print</button>
        </div>
      </div>
      {section("Incoming mail (IMAP)", rows.imap)}
      {section("Outgoing mail (SMTP)", rows.smtp)}
      {!password && (
        <div className="cred-actions">
          <AsyncButton className="btn sm" onClick={async () => setPassword((await api.setPassword(email, "")).password)}>
            <Icon name="key" />Make a new password
          </AsyncButton>
          <span className="muted small">Passwords are stored hashed and can't be shown. A new one stops the old one working in other apps.</span>
        </div>
      )}
    </>
  );
}

function Folders({ account: a }: { account: Account }) {
  return (
    <>
      <div className="table-wrap">
        <table>
          <thead>
            <tr><th>Folder</th><th className="num">Messages</th><th className="num">Unread</th><th className="num">Size</th></tr>
          </thead>
          <tbody>
            {(a.mailboxes ?? []).map((m) => (
              <tr key={m.name}>
                <td><span className="user-cell"><Icon name="folder" />{m.name}</span></td>
                <td className="num">{m.messages.toLocaleString()}</td>
                <td className="num">{m.unseen.toLocaleString()}</td>
                <td className="num">{fmtSize(m.size)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="muted small">{a.aliases.length ? "Aliases: " + a.aliases.join(", ") : "No aliases. Add them on the Aliases page."}</p>
    </>
  );
}

export function NewAccount({ domains, onCreated }: { domains: string[]; onCreated: () => Promise<void> }) {
  const { open } = useModal();
  const [name, setName] = useState("");
  const [domain, setDomain] = useState(domains[0] ?? "");
  const [password, setPassword] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  const create = async () => {
    const r = await api.createAccount(name.includes("@") ? name : name + "@" + domain, password);
    await onCreated();
    open(<PasswordModal title="Account created" email={r.email} password={r.password} />);
  };
  return (
    <Modal title="New account" footer={<><CloseButton label="Cancel" /><AsyncButton className="btn primary" onClick={create}>Create</AsyncButton></>}>
      <div className="field">
        <label htmlFor="new-acct">Address</label>
        <div className="input-suffix">
          <input id="new-acct" ref={input} type="text" placeholder="name" spellCheck={false} value={name} onChange={(e) => setName(e.target.value)} />
          <span>@{domain || "…"}</span>
        </div>
      </div>
      <DomainField domains={domains} value={domain} onChange={setDomain} />
      <PasswordField value={password} onChange={setPassword} />
    </Modal>
  );
}

// SetPassword replaces an account's password; the old one stops working.
function SetPassword({ email }: { email: string }) {
  const { open } = useModal();
  const [password, setPassword] = useState("");
  const save = async () => {
    const r = await api.setPassword(email, password);
    open(<PasswordModal title="Password set" email={r.email} password={r.password} />);
  };
  return (
    <Modal title={"Set password for " + email}
      footer={<><CloseButton label="Cancel" /><AsyncButton className="btn primary" onClick={save}>Set password</AsyncButton></>}>
      <PasswordField value={password} onChange={setPassword} />
      <p className="muted small">The current password stops working right away.</p>
    </Modal>
  );
}
