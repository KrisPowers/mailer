import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { AsyncButton } from "../components/AsyncButton";
import { Badge } from "../components/Badge";
import { Confirm } from "../components/Confirm";
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
    {
      label: "New password", icon: "key", onClick: () => open(
        <Confirm title="New password" label="Generate"
          message={`Generate a new password for ${a.email}? The current one stops working immediately.`}
          onYes={async () => {
            const r = await api.resetPassword(a.email);
            open(<PasswordModal title="New password" email={r.email} password={r.password} />);
          }} />),
    },
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
        <button className="btn primary" disabled={!ov?.domain} onClick={() => open(<NewAccount domain={ov?.domain ?? ""} onCreated={refresh} />)}>
          <Icon name="plus" />New account
        </button>
      </PageHead>
      <Panel>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Account</th><th>Aliases</th><th className="num">Messages</th>
                <th className="num">Unread</th><th className="num">Size</th><th />
              </tr>
            </thead>
            <tbody>
              {error ? <EmptyRow cols={6}>{error}</EmptyRow>
                : !data ? <EmptyRow cols={6}>Loading…</EmptyRow>
                : !list.length ? <EmptyRow cols={6}>{accounts.length ? "No matches." : "No accounts yet."}</EmptyRow>
                : list.map((a) => (
                  <tr key={a.email} className="clickable" onClick={() => open(<AccountDetails account={a} />)}>
                    <td><div className="user-cell"><span className="avatar">{a.email[0]}</span>{a.email}</div></td>
                    <td>{a.aliases.length ? <Badge kind="plain">{plural(a.aliases.length, "alias", "aliases")}</Badge> : <span className="muted">—</span>}</td>
                    <td className="num">{a.messages.toLocaleString()}</td>
                    <td className="num">{a.unseen ? <b>{a.unseen.toLocaleString()}</b> : "0"}</td>
                    <td className="num">{fmtSize(a.size)}</td>
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

function NewAccount({ domain, onCreated }: { domain: string; onCreated: () => Promise<void> }) {
  const { open } = useModal();
  const [name, setName] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  const create = async () => {
    const r = await api.createAccount(name);
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
        <div className="hint">A strong password is generated and shown once.</div>
      </div>
    </Modal>
  );
}
