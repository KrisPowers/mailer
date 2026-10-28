import { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { PopoverButton } from "../components/Floating";
import { Icon, type IconName } from "../components/Icon";
import { useModal } from "../components/Modal";
import { useToast } from "../components/Toasts";
import { useOverview } from "../overview";
import type { View } from "../router";
import type { Account } from "../types";
import { AccountDetails, NewAccount } from "../views/Accounts";
import { ClientPopover } from "./StatusPopovers";

export function Topbar({ onToggleNav }: { onToggleNav: () => void }) {
  const { ov, reload } = useOverview();
  const toast = useToast();
  const { open } = useModal();
  const [checking, setChecking] = useState(false);

  const recheck = async () => {
    setChecking(true);
    try {
      await api.check();
      await reload();
      toast("Connections re-checked");
    } catch (err) {
      toast((err as Error).message, true);
    } finally {
      setChecking(false);
    }
  };

  return (
    <header className="topbar">
      <button className="icon-btn nav-toggle" title="Toggle menu" onClick={onToggleNav}><Icon name="menu" /></button>
      <div className="brand"><Icon name="mail" /><span><b>mailer</b></span></div>
      <span className="chip">{ov?.domain || "not set up"}</span>
      <div className="top-right">
        <HeaderSearch />
        <PopoverButton title="Mail app credentials" content={<ClientPopover />}>
          <Icon name="key" />
        </PopoverButton>
        <button className={"icon-btn" + (checking ? " spin" : "")} title="Re-check connections" disabled={checking} onClick={recheck}>
          <Icon name="refresh" />
        </button>
        <a className="icon-btn" title="Settings" href="#settings"><Icon name="settings" /></a>
        <button className="hdr-btn" disabled={!ov?.domain}
          onClick={() => open(<NewAccount domains={ov?.domains ?? []} onCreated={async () => { await reload(); }} />)}>
          New account
        </button>
      </div>
    </header>
  );
}

const PAGES: { view: View; label: string; icon: IconName }[] = [
  { view: "dashboard", label: "Dashboard", icon: "grid" },
  { view: "accounts", label: "Accounts", icon: "users" },
  { view: "domains", label: "Domains", icon: "globe" },
  { view: "aliases", label: "Aliases", icon: "at" },
  { view: "queue", label: "Outbound queue", icon: "send" },
  { view: "signins", label: "Sign-ins", icon: "key" },
  { view: "logs", label: "Logs", icon: "terminal" },
  { view: "settings", label: "Settings", icon: "settings" },
];

interface Result {
  key: string;
  label: string;
  icon: IconName;
  hint: string;
  go: () => void;
}

// HeaderSearch jumps to a page or opens an account's credentials. Ctrl+K
// focuses it.
function HeaderSearch() {
  const { open } = useModal();
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
  const results: Result[] = [
    ...PAGES.filter((p) => p.label.toLowerCase().includes(query)).map((p) => ({
      key: "p" + p.view, label: p.label, icon: p.icon, hint: "Page", go: () => { location.hash = p.view; },
    })),
    ...accounts.filter((a) => a.email.includes(query) || a.aliases.some((x) => x.includes(query))).map((a) => ({
      key: "a" + a.email, label: a.email, icon: "users" as IconName, hint: "Credentials",
      go: () => open(<AccountDetails account={a} />),
    })),
  ].slice(0, 12);

  const pick = (r?: Result) => {
    if (!r) return;
    r.go();
    setQ("");
    input.current?.blur();
  };

  return (
    <div className="hdr-search">
      <Icon name="search" />
      <input ref={input} type="search" placeholder="Search" aria-label="Search pages and accounts" value={q}
        onFocus={() => { setFocused(true); setSel(0); api.accounts().then(setAccounts).catch(() => {}); }}
        onBlur={() => setTimeout(() => setFocused(false), 150)}
        onChange={(e) => { setQ(e.target.value); setSel(0); }}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") { e.preventDefault(); setSel((s) => Math.min(s + 1, results.length - 1)); }
          if (e.key === "ArrowUp") { e.preventDefault(); setSel((s) => Math.max(s - 1, 0)); }
          if (e.key === "Enter") pick(results[sel]);
          if (e.key === "Escape") input.current?.blur();
        }} />
      {!focused && <span className="kbd">Ctrl+K</span>}
      {focused && (
        <div className="search-results" role="listbox">
          {results.length ? results.map((r, i) => (
            <button key={r.key} className={i === sel ? "sel" : ""} onMouseDown={(e) => e.preventDefault()} onClick={() => pick(r)}>
              <Icon name={r.icon} /><span>{r.label}</span><em>{r.hint}</em>
            </button>
          )) : <div className="empty">No pages or accounts match.</div>}
        </div>
      )}
    </div>
  );
}
