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
import { EmptyRow, PageHead, Panel, Search } from "../components/Layout";
import { CloseButton, Modal, useModal } from "../components/Modal";
import { useCopy, useToast } from "../components/Toasts";
import { fmtSize, plural } from "../format";
import { useOverview } from "../overview";
import { ClientSettings } from "../setup/ClientSettings";
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

  const menu = (a: Account) => (): MenuItem[] => [
    { label: "View folders", icon: "folder", onClick: () => open(<AccountDetails account={a} />) },
    { label: "Client settings", icon: "phone", onClick: () => open(<Modal title="Client settings" footer={<CloseButton />}><ClientSettings /></Modal>) },
    { label: "Copy address", icon: "copy", onClick: () => copy(a.email, "Address copied") },
    "-",
    { label: "Set password", icon: "key", onClick: () => open(<SetPassword email={a.email} />) },
    {
      label: "Delete account", icon: "trash", danger: true, onClick: () => open(
        <Confirm title="Delete account" label="Delete" danger
          message={`Delete ${a.email}? It can no longer sign in. Stored mail stays on disk.`}
          onYes={async () => {
            await api.deleteAccount(a.email);
            toast("Deleted " + a.email);
            await refresh();
          }} />),
    },
  ];

  return (
    <>
      <PageHead title="Accounts" sub={data ? accounts.length + " total" : ""}>
        <Search value={q} onChange={setQ} placeholder="Search accounts" />
        <button className="btn primary" disabled={!ov?.domain} onClick={() => open(<NewAccount domains={ov?.domains ?? []} onCreated={refresh} />)}>
          <Icon name="plus" />New account
        </button>
      </PageHead>
      <Panel>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Account</th><th>Aliases</th><th className="num">Messages</th>
                <th className="num">Unread</th><th className="num">Size</th><th /><th />
              </tr>
            </thead>
            <tbody>
              {error ? <EmptyRow cols={7}>{error}</EmptyRow>
                : !data ? <EmptyRow cols={7}>Loading…</EmptyRow>
                : !list.length ? <EmptyRow cols={7}>{accounts.length ? "No matches." : "No accounts yet."}</EmptyRow>
                : list.map((a) => (
                  <tr key={a.email} className="clickable" onClick={() => open(<AccountDetails account={a} />)}>
                    <td><div className="user-cell"><span className="avatar">{a.email[0]}</span>{a.email}</div></td>
                    <td>{a.aliases.length ? <Badge kind="plain">{plural(a.aliases.length, "alias", "aliases")}</Badge> : <span className="muted">—</span>}</td>
                    <td className="num">{a.messages.toLocaleString()}</td>
                    <td className="num">{a.unseen ? <b>{a.unseen.toLocaleString()}</b> : "0"}</td>
                    <td className="num">{fmtSize(a.size)}</td>
                    <td className="act" onClick={(e) => e.stopPropagation()}>
                      <button className="icon-btn" title="Add to Outlook" onClick={() => open(<AddToOutlook email={a.email} />)}>
                        <Icon name="mail" />
                      </button>
                    </td>
                    <td className="act" onClick={(e) => e.stopPropagation()}><MoreMenu items={menu(a)} /></td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      </Panel>
    </>
  );
}

function AccountDetails({ account: a }: { account: Account }) {
  return (
    <Modal title={a.email} wide footer={<CloseButton />}>
      <div className="panel">
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
      </div>
      <p className="muted small">{a.aliases.length ? "Aliases: " + a.aliases.join(", ") : "No aliases. Add them on the Aliases page."}</p>
    </Modal>
  );
}

function NewAccount({ domains, onCreated }: { domains: string[]; onCreated: () => Promise<void> }) {
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

// AddToOutlook walks through adding an account to the Outlook app. Outlook
// finds the server and ports itself through Autodiscover, so the address
// and password are all it needs; the password can't be read back, so a new
// one can be made here to paste.
function AddToOutlook({ email }: { email: string }) {
  const [password, setPassword] = useState("");
  const [manual, setManual] = useState(false);
  return (
    <Modal title={"Add " + email + " to Outlook"} wide footer={<CloseButton label="Done" />}>
      <ol className="perm-list">
        <li>In the Outlook app, add an account: <b>File → Add Account</b> in classic Outlook, or
          <b> Settings → Accounts → Add account</b> in the new Outlook.</li>
        <li>Paste the address and continue.
          <div className="secret-box"><span>{email}</span><CopyButton text={email} label="Address copied" /></div>
        </li>
        <li>Paste the password and connect. Outlook fills in the server and ports by itself.
          {password
            ? <div className="secret-box"><span>{password}</span><CopyButton text={password} label="Password copied" /></div>
            : <div>
              <AsyncButton className="btn sm" onClick={async () => setPassword((await api.setPassword(email, "")).password)}>
                <Icon name="key" />Make a new password
              </AsyncButton>
              <div className="muted small">Use the current one if you have it. A new one stops the old one working in other apps.</div>
            </div>}
        </li>
      </ol>
      <p className="muted small">
        Use the Outlook app: outlook.com in a browser can't add other mail servers. If Outlook asks for the account type,
        choose <b>IMAP</b>{manual ? " and enter:" : <> (<a href="#" onClick={(e) => { e.preventDefault(); setManual(true); }}>show the settings</a>).</>}
      </p>
      {manual && <ClientSettings />}
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
