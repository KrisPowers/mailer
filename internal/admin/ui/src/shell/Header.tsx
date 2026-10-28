import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { MailSettings } from "../flows/MailSettings";
import { NewAccount } from "../flows/Accounts";
import { Setup } from "../flows/Setup";
import { useOverview } from "../overview";
import { href, type View } from "../router";
import { signedOut, useUser } from "../session";
import type { Account } from "../types";
import { Button, IconButton } from "../ui/Button";
import { PopoverButton } from "../ui/Floating";
import { Icon, type IconName } from "../ui/Icon";
import { useModal } from "../ui/Modal";
import { useToast } from "../ui/Toast";

export function Header({ onToggleNav }: { onToggleNav: () => void }) {
  const { ov, reload } = useOverview();
  const { open } = useModal();
  const toast = useToast();
  return (
    <header className="header">
      <IconButton className="nav-toggle" icon="left_panel_open" label="Menu" onClick={onToggleNav} />
      <a className="brand" href={href("accounts")}>
        <Icon name="mail" size={26} />
        <span>mailer</span>
      </a>
      <CommandSearch />
      <div className="header-icons">
        <IconButton icon="key" label="Mail app settings" onClick={() => open(<MailSettings />)} />
        <IconButton icon="refresh" label="Re-check connections" onClick={async () => {
          await api.check();
          await reload();
          toast("Connections re-checked");
        }} />
        <a className="icon-btn" href={href("settings")} aria-label="Settings" data-tip="Settings"><Icon name="settings" /></a>
        <UserMenu />
      </div>
      <button type="button" className="header-btn"
        onClick={() => open(ov?.domain ? <NewAccount /> : <Setup />)}>
        {ov?.domain ? "New account" : "Set up"}
      </button>
    </header>
  );
}

// UserMenu shows who's signed in, and signs out.
function UserMenu() {
  const user = useUser();
  if (!user) return null;
  const signOut = async () => {
    await api.logout().catch(() => {});
    signedOut();
  };
  return (
    <PopoverButton label={"Signed in as " + user.name} content={
      <div className="popover-body user-pop">
        <h3>{user.name}</h3>
        <p className="muted">{user.role === "admin" ? "Admin" : "User"}</p>
        <div className="user-pop-actions">
          <a className="btn small" href={href("settings")}>Change password</a>
          <Button small icon="logout" onClick={signOut}>Sign out</Button>
        </div>
      </div>
    }>
      <Icon name="person" />
    </PopoverButton>
  );
}

const PAGES: { view: View; label: string; icon: IconName; admin?: boolean }[] = [
  { view: "accounts", label: "Accounts", icon: "group" },
  { view: "queue", label: "Queue", icon: "outbox" },
  { view: "logs", label: "Logs", icon: "terminal" },
  { view: "access", label: "Access", icon: "devices", admin: true },
  { view: "settings", label: "Settings", icon: "settings" },
];

// CommandSearch finds pages and accounts; Ctrl+K focuses it.
function CommandSearch() {
  const admin = useUser()?.role === "admin";
  const [q, setQ] = useState("");
  const [focused, setFocused] = useState(false);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [sel, setSel] = useState(0);
  const input = useRef<HTMLInputElement>(null);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        input.current?.focus();
      }
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, []);

  const query = q.trim().toLowerCase();
  const results = [
    ...PAGES.filter((p) => (admin || !p.admin) && p.label.toLowerCase().includes(query)).map((p) => ({ key: p.view, label: p.label, icon: p.icon, hint: "Page", to: href(p.view) })),
    ...accounts.filter((a) => a.email.includes(query) || a.aliases.some((x) => x.includes(query)))
      .map((a) => ({ key: a.email, label: a.email, icon: "person" as IconName, hint: "Account", to: href("accounts", a.email) })),
  ].slice(0, 10);

  const go = (to?: string) => {
    if (!to) return;
    location.hash = to;
    setQ("");
    input.current?.blur();
  };

  return (
    <div className="command">
      <Icon name="search" />
      <input ref={input} type="text" role="combobox" aria-expanded={focused} aria-controls="command-results" aria-label="Search pages and accounts"
        placeholder="Search" spellCheck={false} value={q}
        onFocus={() => { setFocused(true); setSel(0); api.accounts().then(setAccounts).catch(() => {}); }}
        onBlur={() => setFocused(false)}
        onChange={(e) => { setQ(e.target.value); setSel(0); }}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") { e.preventDefault(); setSel((s) => Math.min(s + 1, results.length - 1)); }
          if (e.key === "ArrowUp") { e.preventDefault(); setSel((s) => Math.max(s - 1, 0)); }
          if (e.key === "Enter") go(results[sel]?.to);
          if (e.key === "Escape") input.current?.blur();
        }} />
      {!focused && <kbd>Ctrl+K</kbd>}
      {focused && (
        <div className="command-results" id="command-results" role="listbox">
          {results.length ? results.map((r, i) => (
            <button key={r.key} type="button" role="option" aria-selected={i === sel} className={i === sel ? "sel" : ""}
              onMouseDown={(e) => { e.preventDefault(); go(r.to); }} onMouseEnter={() => setSel(i)}>
              <Icon name={r.icon} size={18} /><span>{r.label}</span><em>{r.hint}</em>
            </button>
          )) : <div className="command-empty">No pages or accounts match "{q}"</div>}
        </div>
      )}
    </div>
  );
}
