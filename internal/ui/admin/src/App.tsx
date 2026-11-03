import { useCallback, useEffect, useRef, useState, type KeyboardEvent as KeyEvent } from "react";
import { Icon, type IconName } from "../../shared/Icon";
import { Callout, MoreMenu, Notices } from "../../shared/ui";
import { AccountPage, Accounts, NewAccount } from "./Accounts";
import { api } from "./api";
import { Flagged } from "./Flagged";
import { Gate } from "./Gate";
import { HELP, Help } from "./Help";
import { Logs } from "./Logs";
import { failing, OverviewProvider, setupStep, useOverview } from "./overview";
import { Devices, NewPerson, People } from "./People";
import { accountHref, go, readHash } from "./route";
import { signedOut, useUser } from "./session";
import { SETTINGS, Settings } from "./Settings";
import { Setup } from "./Setup";
import { Status } from "./Status";
import { Traffic } from "./Traffic";
import type { Account } from "./types";

// Nothing past the Gate loads until this device is approved and someone
// has signed in on it.
export function App() {
  return (
    <Notices>
      <Gate>
        <OverviewProvider>
          <Dashboard />
        </OverviewProvider>
      </Gate>
    </Notices>
  );
}

const signOut = async () => {
  await api.logout().catch(() => {});
  signedOut();
};

// Dashboard is the frame every page sits in: a header with the name, the
// search bar, and new account, help and settings; a sidebar with the pages;
// and the page.
function Dashboard() {
  const { ov, error } = useOverview();
  const user = useUser();
  const admin = user?.role === "admin";
  const [place, setPlace] = useState(readHash);
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [drawer, setDrawer] = useState(false);
  const sentToSetup = useRef(false);

  const loadAccounts = useCallback(async () => {
    try {
      setAccounts(await api.accounts());
    } catch {
      // The overview's error says the server is unreachable.
    }
  }, []);

  useEffect(() => {
    loadAccounts();
    const t = setInterval(loadAccounts, 20000);
    return () => clearInterval(t);
  }, [loadAccounts]);

  useEffect(() => {
    const onHash = () => {
      setPlace(readHash());
      setDrawer(false);
      scrollTo(0, 0);
    };
    addEventListener("hashchange", onHash);
    return () => removeEventListener("hashchange", onHash);
  }, []);

  // Until setup is done, the first visit of a browser session opens it.
  useEffect(() => {
    if (!ov || sentToSetup.current) return;
    sentToSetup.current = true;
    let shown = false;
    try {
      shown = sessionStorage.getItem("mailer.setup") === "1";
      sessionStorage.setItem("mailer.setup", "1");
    } catch {
      // Storage unavailable: open it this once.
    }
    if (setupStep(ov) && !shown && location.hash.replace(/^#\/?/, "") === "") go("#/setup");
  }, [ov]);

  const { view, id, page } = place;
  const account = view === "accounts" && id ? accounts?.find((a) => a.email === id) : undefined;
  const problems = failing(ov).length;
  const sections = view === "settings" ? SETTINGS.filter((s) => admin || !s[3]) : view === "help" ? HELP : null;
  const changed = async () => {
    await loadAccounts();
  };

  return (
    <div className="app">
      <header className="header">
        <div className="header-start">
          <button type="button" className="icon-btn menu-btn" aria-label="Menu" aria-expanded={drawer} onClick={() => setDrawer((d) => !d)}>
            <Icon name="menu" />
          </button>
          <a className="brand" href="#/"><span className="brand-mark"><Icon name="mail" size={20} /></span><span className="brand-name">Mailer</span></a>
        </div>
        <Search accounts={accounts} admin={admin} />
        <div className="header-actions">
          <a className={"icon-btn" + (view === "new" ? " on" : "")} href="#/new" aria-label="New account" title="New account"><Icon name="add" /></a>
          <a className={"icon-btn" + (view === "help" ? " on" : "")} href="#/help" aria-label="Help" title="Help"><Icon name="help" /></a>
          <a className={"icon-btn" + (view === "settings" ? " on" : "")} href="#/settings" aria-label="Settings" title="Settings"><Icon name="settings" /></a>
          {user && (
            <MoreMenu label={`Signed in as ${user.name}`} icon="person" actions={[
              { label: `${user.name}, ${admin ? "admin" : "user"}`, icon: "person", disabled: true, onClick: () => {} },
              { label: "Change password", icon: "lock_reset", onClick: () => go("#/settings/account") },
              { label: "Sign out", icon: "logout", onClick: signOut },
            ]} />
          )}
        </div>
      </header>

      <div className="shell">
        <nav className={"sidebar" + (drawer ? " open" : "")} aria-label="Pages">
          {sections ? (
            <>
              <a className="nav-back" href="#/"><Icon name="arrow_back" /><span>Dashboard</span></a>
              <hr className="nav-rule" />
              {sections.map(([key, label, icon], i) => (
                <NavItem key={key} to={`#/${view}/${key}`} icon={icon} label={label} on={id === key || (!id && i === 0)} quiet />
              ))}
            </>
          ) : (
            <>
              {setupStep(ov) > 0 && <NavItem to="#/setup" icon="build" label="Finish setup" on={view === "setup"} />}
              <NavItem to="#/" icon="group" label="Accounts" on={view === "accounts" || view === "new"} count={accounts?.length} />
              <NavItem to="#/traffic" icon="outbox" label="Traffic" on={view === "traffic"} count={ov?.queue || undefined} alert />
              <NavItem to="#/flagged" icon="flag" label="Flagged" on={view === "flagged"} count={ov?.flagged || undefined} />
              <NavItem to="#/status" icon="lan" label="Status" on={view === "status"} count={problems || undefined} alert />
              <NavItem to="#/logs" icon="terminal" label="Logs" on={view === "logs"} />
              {admin && (
                <>
                  <hr className="nav-rule" />
                  <NavItem to="#/devices" icon="devices" label="Devices" on={view === "devices"} count={ov?.pending_devices || undefined} alert />
                  <NavItem to="#/people" icon="shield" label="People" on={view === "people"} />
                </>
              )}
            </>
          )}
        </nav>
        {drawer && <div className="scrim" onClick={() => setDrawer(false)} />}

        <main className={"page" + (view === "help" ? " wide" : "")} key={view + id}>
          {error && !ov && <Callout tone="bad" icon="error">Can't reach the mail server: {error}</Callout>}
          {view === "accounts" && !id && <Accounts accounts={accounts} />}
          {view === "accounts" && id && (account
            ? <AccountPage account={account} page={page} onChanged={changed} />
            : <Callout icon="info">{accounts ? <>No account {id}. <a href="#/">All accounts</a></> : "Loading"}</Callout>)}
          {view === "new" && <NewAccount onAdded={changed} />}
          {view === "traffic" && <Traffic />}
          {view === "flagged" && <Flagged />}
          {view === "status" && <Status />}
          {view === "logs" && <Logs />}
          {view === "devices" && admin && <Devices />}
          {view === "people" && admin && (id === "new" ? <NewPerson /> : <People />)}
          {view === "setup" && <Setup step={id} />}
          {view === "settings" && <Settings section={id} onSignOut={signOut} />}
          {view === "help" && <Help guide={id} heading={page} />}
          {(view === "devices" || view === "people") && !admin && <Callout icon="lock">Only an admin can manage devices and people.</Callout>}
        </main>
      </div>
    </div>
  );
}

function NavItem({ to, icon, label, on, quiet, count, alert }: { to: string; icon: IconName; label: string; on: boolean; quiet?: boolean; count?: number; alert?: boolean }) {
  return (
    <a className={"nav-item" + (quiet ? " quiet" : "") + (on ? " on" : "")} href={to} aria-current={on ? "page" : undefined}>
      <Icon name={icon} />
      <span className="nav-label">{label}</span>
      {count !== undefined && <span className={"nav-count" + (alert ? " alert" : "")}>{count.toLocaleString()}</span>}
    </a>
  );
}

const PAGES: { label: string; icon: IconName; to: string; admin?: boolean }[] = [
  { label: "Accounts", icon: "group", to: "#/" },
  { label: "New account", icon: "add", to: "#/new" },
  { label: "Traffic", icon: "outbox", to: "#/traffic" },
  { label: "Flagged", icon: "flag", to: "#/flagged" },
  { label: "Status", icon: "lan", to: "#/status" },
  { label: "Logs", icon: "terminal", to: "#/logs" },
  { label: "Devices", icon: "devices", to: "#/devices", admin: true },
  { label: "People", icon: "shield", to: "#/people", admin: true },
  { label: "Setup", icon: "build", to: "#/setup" },
  { label: "Settings", icon: "settings", to: "#/settings" },
  { label: "Help", icon: "help", to: "#/help" },
];

// Search finds an account by any part of its address or an alias, or a page,
// and opens it. Ctrl+K focuses it.
function Search({ accounts, admin }: { accounts: Account[] | null; admin: boolean }) {
  const [q, setQ] = useState("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
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

  const needle = q.trim().toLowerCase();
  const items: { key: string; icon: IconName; label: string; sub: string; to: string }[] = needle ? [
    ...(accounts ?? [])
      .filter((a) => a.email.includes(needle) || a.aliases.some((x) => x.includes(needle)))
      .map((a) => ({ key: "a" + a.email, icon: "person" as IconName, label: a.email, sub: "Account", to: accountHref(a.email) })),
    ...PAGES.filter((p) => (admin || !p.admin) && p.label.toLowerCase().includes(needle))
      .map((p) => ({ key: "p" + p.to, icon: p.icon, label: p.label, sub: "Page", to: p.to })),
  ].slice(0, 8) : [];
  const listed = open && items.length > 0;

  const choose = (to: string) => {
    setQ("");
    setOpen(false);
    input.current?.blur();
    go(to);
  };

  const onKeyDown = (e: KeyEvent) => {
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      const step = e.key === "ArrowDown" ? 1 : -1;
      setActive((a) => Math.max(0, Math.min(items.length - 1, a + step)));
    } else if (e.key === "Escape") {
      if (listed) setOpen(false);
      else input.current?.blur();
    }
  };

  return (
    <form className="search" role="search" autoComplete="off" onSubmit={(e) => {
      e.preventDefault();
      if (items[active]) choose(items[active].to);
    }}>
      <Icon name="search" className="search-icon" />
      <input ref={input} type="text" aria-label="Search accounts and pages" placeholder="Search accounts and pages"
        role="combobox" aria-expanded={listed} aria-controls="found" aria-autocomplete="list"
        aria-activedescendant={listed ? "found-" + active : undefined}
        autoComplete="off" spellCheck={false} data-1p-ignore data-lpignore="true"
        value={q} onFocus={() => setOpen(true)} onBlur={() => setOpen(false)} onKeyDown={onKeyDown}
        onChange={(e) => {
          setQ(e.target.value);
          setOpen(true);
          setActive(0);
        }} />
      <kbd>Ctrl+K</kbd>
      {listed && (
        <div className="recent">
          <ul id="found" role="listbox" aria-label="Matches">
            {items.map((item, i) => (
              <li key={item.key} id={"found-" + i} role="option" aria-selected={i === active}
                className={"recent-item" + (i === active ? " active" : "")}
                onMouseDown={(e) => e.preventDefault()} onMouseEnter={() => setActive(i)} onClick={() => choose(item.to)}>
                <Icon name={item.icon} size={18} />
                <span className="recent-target">{item.label}</span>
                <span className="recent-when">{item.sub}</span>
              </li>
            ))}
          </ul>
        </div>
      )}
    </form>
  );
}
