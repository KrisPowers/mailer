import { useCallback, useEffect, useRef, useState, type KeyboardEvent as KeyEvent } from "react";
import { Icon, type IconName } from "../../shared/Icon";
import { Callout, MoreMenu, Notices, Tooltips, useNav } from "../../shared/ui";
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
      <Tooltips />
    </Notices>
  );
}

const signOut = async () => {
  await api.logout().catch(() => {});
  signedOut();
};

// Dashboard is the frame every page sits in: a header with the menu, the
// name, the search bar and who is signed in; a sidebar with the pages, Help
// and Settings at its foot; and the page.
function Dashboard() {
  const { ov, error } = useOverview();
  const user = useUser();
  const admin = user?.role === "admin";
  const [place, setPlace] = useState(readHash);
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const { drawer, setDrawer, rail, menu } = useNav();
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
  }, [setDrawer]);

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
    <div className={"app" + (rail ? " rail" : "")}>
      <header className="header">
        <div className="header-start">
          <button type="button" className="icon-btn menu-btn" aria-label="Menu" aria-expanded={drawer || !rail} onClick={menu}>
            <Icon name="menu" />
          </button>
          <a className="brand" href="#/"><span className="brand-mark"><Icon name="mail" size={18} /></span><span className="brand-name">Mailer</span></a>
        </div>
        <Search accounts={accounts} admin={admin} />
        <div className="header-actions">
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
              <a className="nav-back" href="#/" title={rail ? "Back" : undefined}><Icon name="arrow_back" /><span className="nav-label">Back</span></a>
              {sections.map(([key, label, icon], i) => (
                <NavItem key={key} to={`#/${view}/${key}`} icon={icon} label={label} on={id === key || (!id && i === 0)} quiet rail={rail} />
              ))}
            </>
          ) : (
            <>
              {setupStep(ov) > 0 && <NavItem to="#/setup" icon="build" label="Finish setup" on={view === "setup"} rail={rail} />}
              <NavItem to="#/" icon="group" label="Accounts" on={view === "accounts" || view === "new"} rail={rail} />
              <NavItem to="#/traffic" icon="outbox" label="Traffic" on={view === "traffic"} count={ov?.queue || undefined} rail={rail} />
              <NavItem to="#/flagged" icon="flag" label="Flagged" on={view === "flagged"} count={ov?.flagged || undefined} rail={rail} />
              <NavItem to="#/status" icon="lan" label="Status" on={view === "status"} count={problems || undefined} rail={rail} />
              <NavItem to="#/logs" icon="terminal" label="Logs" on={view === "logs"} rail={rail} />
              {admin && <NavItem to="#/devices" icon="devices" label="Devices" on={view === "devices"} count={ov?.pending_devices || undefined} rail={rail} />}
              {admin && <NavItem to="#/people" icon="shield" label="People" on={view === "people"} rail={rail} />}
            </>
          )}
          <span className="nav-gap" />
          <NavItem to="#/help" icon="help" label="Help" on={view === "help"} rail={rail} />
          <NavItem to="#/settings" icon="settings" label="Settings" on={view === "settings"} rail={rail} />
        </nav>
        {drawer && <div className="scrim" onClick={() => setDrawer(false)} />}

        {/* A new page fades in; a section of the same page changes in place. */}
        <main className="page" key={view === "accounts" ? view + id : view}>
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

// NavItem is a page, or a section of settings or help, in the sidebar; folded
// to its icon, its name shows on hover.
function NavItem({ to, icon, label, on, quiet, count, rail }: { to: string; icon: IconName; label: string; on: boolean; quiet?: boolean; count?: number; rail?: boolean }) {
  return (
    <a className={"nav-item" + (quiet ? " quiet" : "") + (on ? " on" : "")} href={to} aria-current={on ? "page" : undefined} title={rail ? label : undefined}>
      <Icon name={icon} />
      <span className="nav-label">{label}</span>
      {count !== undefined && <span className="nav-count">{count.toLocaleString()}</span>}
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
