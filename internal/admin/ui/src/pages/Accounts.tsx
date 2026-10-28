import { Fragment, useEffect, useState } from "react";
import { api } from "../api";
import { NewAccount, PasswordShown, SetPassword } from "../flows/Accounts";
import { fmtSize, plural } from "../format";
import { useOverview } from "../overview";
import { href } from "../router";
import type { Account } from "../types";
import { IconButton } from "../ui/Button";
import { Menu, type MenuItem } from "../ui/Floating";
import { Icon } from "../ui/Icon";
import { Confirm, useModal } from "../ui/Modal";
import { Chip, Dot, EmptyRow, PageHeader } from "../ui/Page";
import { useCopy, useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";
import { AccountPage, lockedText } from "./AccountPage";

// Accounts is the table of mailboxes and nothing else; each row opens the
// account's own page.
export function Accounts({ param }: { param: string }) {
  const { ov, reload: reloadOverview } = useOverview();
  const { data, error, reload } = useLoad(api.accounts);
  const { open } = useModal();
  const copy = useCopy();
  const toast = useToast();
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  // Follow the overview's refresh, which runs after creating an account.
  useEffect(() => { reload(); }, [ov]);

  const accounts = data ?? [];
  if (param) {
    const a = accounts.find((x) => x.email === param);
    return a ? <AccountPage account={a} onChanged={reload} />
      : <><PageHeader title={param} back={href("accounts")} /><p className="muted">{data ? "No such account." : "Loading…"}</p></>;
  }

  const remove = (a: Account) => open(
    <Confirm title="Delete account" label="Delete"
      message={<>Delete <b>{a.email}</b>? It can no longer sign in. Stored mail stays on disk.</>}
      onYes={async () => {
        await api.deleteAccount(a.email);
        toast("Deleted " + a.email);
        await Promise.all([reload(), reloadOverview()]);
      }} />);

  const menu = (a: Account) => (): MenuItem[] => [
    { label: "Copy address", icon: "content_copy", onClick: () => copy(a.email, "Address copied") },
    { label: "Set a new password", icon: "lock_reset", onClick: () => open(<SetPassword email={a.email} />) },
    { label: "Generate a password now", icon: "key", onClick: async () => {
      const r = await api.setPassword(a.email, "");
      open(<PasswordShown title="Password set" email={r.email} password={r.password} />);
    } },
  ];

  const flip = (email: string) => {
    const next = new Set(expanded);
    if (next.has(email)) next.delete(email); else next.add(email);
    setExpanded(next);
  };

  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th className="col-expand" /><th className="col-dot" />
            <th>Name</th><th>Aliases</th><th className="num">Messages</th><th className="num">Unread</th><th className="num grow">Size</th>
            <th className="col-actions">Actions</th>
          </tr>
        </thead>
        <tbody>
          {error ? <EmptyRow cols={8} icon="error">{error}</EmptyRow>
            : !data ? <EmptyRow cols={8}>Loading…</EmptyRow>
            : !accounts.length ? <EmptyRow cols={8} icon="group">
              No accounts yet. <button type="button" className="link-btn" onClick={() => open(<NewAccount />)}>Create one</button>
            </EmptyRow>
            : accounts.map((a) => (
              <Fragment key={a.email}>
                <tr>
                  <td className="col-expand">
                    <IconButton icon={expanded.has(a.email) ? "expand_more" : "chevron_right"} tone="accent"
                      label={expanded.has(a.email) ? "Hide folders" : "Show folders"} onClick={() => flip(a.email)} />
                  </td>
                  <td className="col-dot"><Dot health={lockedText(a.locks) ? "warn" : "ok"} label={lockedText(a.locks) || "Active"} /></td>
                  <td><a className="name-link" href={href("accounts", a.email)}>{a.email}</a></td>
                  <td>{a.aliases.length ? <Chip health="plain">{plural(a.aliases.length, "alias", "aliases")}</Chip> : <span className="muted">-</span>}</td>
                  <td className="num">{a.messages.toLocaleString()}</td>
                  <td className="num">{a.unseen ? <b>{a.unseen.toLocaleString()}</b> : "0"}</td>
                  <td className="num">{fmtSize(a.size)}</td>
                  <td className="col-actions">
                    <span className="row-actions">
                      <a className="icon-btn accent" href={href("accounts", a.email)} aria-label="Open account" data-tip="Open account"><Icon name="key" /></a>
                      <Menu items={menu(a)} />
                      <span className="divider" />
                      <IconButton icon="delete" label="Delete account" tone="danger" onClick={() => remove(a)} />
                    </span>
                  </td>
                </tr>
                {expanded.has(a.email) && (a.mailboxes ?? []).map((m) => (
                  <tr key={a.email + m.name} className="child-row">
                    <td className="col-expand" /><td className="col-dot" />
                    <td><span className="with-icon"><Icon name="folder" size={18} />{m.name}</span></td>
                    <td />
                    <td className="num">{m.messages.toLocaleString()}</td>
                    <td className="num">{m.unseen.toLocaleString()}</td>
                    <td className="num">{fmtSize(m.size)}</td>
                    <td className="col-actions" />
                  </tr>
                ))}
              </Fragment>
            ))}
        </tbody>
      </table>
    </div>
  );
}
