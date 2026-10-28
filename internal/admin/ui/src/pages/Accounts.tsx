import { Fragment, useEffect, useState } from "react";
import { api } from "../api";
import { NewAccount, PasswordShown, SetPassword } from "../flows/Accounts";
import { MailSettings } from "../flows/MailSettings";
import { fmtSize, plural } from "../format";
import { useOverview } from "../overview";
import { href } from "../router";
import type { Account } from "../types";
import { Button, IconButton, LinkButton } from "../ui/Button";
import { SearchField } from "../ui/Field";
import { Menu, type MenuItem } from "../ui/Floating";
import { Icon } from "../ui/Icon";
import { Confirm, useModal } from "../ui/Modal";
import { Chip, Dot, EmptyRow, Metrics, PageHeader, Showing, Toolbar } from "../ui/Page";
import { Checkbox, Toggle } from "../ui/Toggle";
import { useCopy, useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";
import { AccountPage } from "./AccountPage";

export function Accounts({ param }: { param: string }) {
  const { ov, reload: reloadOverview } = useOverview();
  const { data, error, reload } = useLoad(api.accounts);
  const { open } = useModal();
  const copy = useCopy();
  const toast = useToast();
  const [q, setQ] = useState("");
  const [unreadOnly, setUnreadOnly] = useState(false);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [expanded, setExpanded] = useState<Set<string>>(new Set());

  // Follow the overview's refresh, which runs after creating an account.
  useEffect(() => { reload(); }, [ov]);

  const accounts = data ?? [];
  if (param) {
    const a = accounts.find((x) => x.email === param);
    return a ? <AccountPage account={a} onChanged={reload} />
      : <><PageHeader title={param} back={href("accounts")} /><p className="muted">{data ? "No such account." : "Loading…"}</p></>;
  }

  const query = q.toLowerCase();
  const list = accounts.filter((a) => (!unreadOnly || a.unseen > 0) && (a.email.includes(query) || a.aliases.some((x) => x.includes(query))));
  const allPicked = list.length > 0 && list.every((a) => picked.has(a.email));
  const flip = (set: Set<string>, key: string, on: boolean) => {
    const next = new Set(set);
    if (on) next.add(key); else next.delete(key);
    return next;
  };
  const refresh = async () => {
    setPicked(new Set());
    await Promise.all([reload(), reloadOverview()]);
  };

  const remove = (emails: string[]) => open(
    <Confirm title={emails.length === 1 ? "Delete account" : "Delete " + emails.length + " accounts"} label="Delete"
      message={<>Delete {emails.length === 1 ? <b>{emails[0]}</b> : plural(emails.length, "account")}? They can no longer sign in. Stored mail stays on disk.</>}
      onYes={async () => {
        for (const e of emails) await api.deleteAccount(e);
        toast(emails.length === 1 ? "Deleted " + emails[0] : "Deleted " + plural(emails.length, "account"));
        await refresh();
      }} />);

  const menu = (a: Account) => (): MenuItem[] => [
    { label: "Copy address", icon: "content_copy", onClick: () => copy(a.email, "Address copied") },
    { label: "New password", icon: "lock_reset", onClick: () => open(<SetPassword email={a.email} />) },
    { label: "Generate a password now", icon: "key", onClick: async () => {
      const r = await api.setPassword(a.email, "");
      open(<PasswordShown title="Password set" email={r.email} password={r.password} />);
    } },
  ];

  const total = accounts.reduce((t, a) => ({ size: t.size + a.size, unread: t.unread + a.unseen }), { size: 0, unread: 0 });

  return (
    <>
      <PageHeader title="Accounts" link={<LinkButton icon="key" onClick={() => open(<MailSettings />)}>Mail app settings</LinkButton>} />
      <Metrics items={[
        { label: "Mailboxes", value: accounts.length },
        { label: "Storage used", value: fmtSize(total.size) },
        { label: "Unread", value: total.unread.toLocaleString() },
      ]} />
      <Toolbar actions={picked.size > 0 && (
        <Button variant="danger" icon="delete" onClick={() => remove([...picked])}>Delete {picked.size}</Button>
      )}>
        <SearchField value={q} onChange={setQ} />
        <Toggle checked={unreadOnly} onChange={setUnreadOnly}>Only show accounts with unread mail</Toggle>
      </Toolbar>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th className="col-check">
                <Checkbox label="Select all" checked={allPicked} mixed={!allPicked && list.some((a) => picked.has(a.email))}
                  onChange={(on) => setPicked(on ? new Set(list.map((a) => a.email)) : new Set())} />
              </th>
              <th className="col-expand" /><th className="col-dot" />
              <th>Name</th><th>Aliases</th><th className="num">Messages</th><th className="num">Unread</th><th className="num">Size</th>
              <th className="col-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            {error ? <EmptyRow cols={9} icon="error">{error}</EmptyRow>
              : !data ? <EmptyRow cols={9}>Loading…</EmptyRow>
              : !list.length ? <EmptyRow cols={9} icon="group">
                {accounts.length ? "No accounts match." : <>No accounts yet. <button type="button" className="link-btn" onClick={() => open(<NewAccount />)}>Create one</button></>}
              </EmptyRow>
              : list.map((a) => (
                <Fragment key={a.email}>
                  <tr className={picked.has(a.email) ? "selected" : ""}>
                    <td className="col-check"><Checkbox label={"Select " + a.email} checked={picked.has(a.email)} onChange={(on) => setPicked(flip(picked, a.email, on))} /></td>
                    <td className="col-expand">
                      <IconButton icon={expanded.has(a.email) ? "expand_more" : "chevron_right"} label={expanded.has(a.email) ? "Hide folders" : "Show folders"} tone="accent"
                        onClick={() => setExpanded(flip(expanded, a.email, !expanded.has(a.email)))} />
                    </td>
                    <td className="col-dot"><Dot health="ok" label="Active" /></td>
                    <td><a className="name-link" href={href("accounts", a.email)}>{a.email}</a></td>
                    <td>{a.aliases.length ? <Chip health="plain">{plural(a.aliases.length, "alias", "aliases")}</Chip> : <span className="muted">-</span>}</td>
                    <td className="num">{a.messages.toLocaleString()}</td>
                    <td className="num">{a.unseen ? <b>{a.unseen.toLocaleString()}</b> : "0"}</td>
                    <td className="num">{fmtSize(a.size)}</td>
                    <td className="col-actions">
                      <span className="row-actions">
                        <a className="icon-btn accent" href={href("accounts", a.email)} aria-label="Credentials" data-tip="Credentials"><Icon name="key" /></a>
                        <Menu items={menu(a)} />
                        <span className="divider" />
                        <IconButton icon="delete" label="Delete account" tone="danger" onClick={() => remove([a.email])} />
                      </span>
                    </td>
                  </tr>
                  {expanded.has(a.email) && (a.mailboxes ?? []).map((m) => (
                    <tr key={a.email + m.name} className="child-row">
                      <td className="col-check" /><td className="col-expand" /><td className="col-dot" />
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
      {data && <Showing count={list.length} />}
    </>
  );
}
