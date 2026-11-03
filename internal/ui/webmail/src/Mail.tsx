import { useCallback, useEffect, useRef, useState, type DragEvent, type FormEvent } from "react";
import { message } from "../../shared/format";
import { Icon, type IconName } from "../../shared/Icon";
import { MoreMenu, useNotify } from "../../shared/ui";
import { api } from "./api";
import { Compose, type Start } from "./Compose";
import { List } from "./List";
import { Reader } from "./Reader";
import { folderHref, go, readHash, viewKey, type View } from "./route";
import { SECTIONS, Settings } from "./Settings";
import type { Folder, Me, Ref, Special } from "./types";

export const FOLDER_ICONS: Record<Special, IconName> = {
  inbox: "inbox", drafts: "draft", sent: "send", archive: "archive", junk: "report", trash: "delete",
};

export const folderIcon = (f?: Folder): IconName => (f?.special ? FOLDER_ICONS[f.special] : "folder");

// folderLabel is a folder's own name, without the folders it sits in.
export const folderLabel = (f: Folder) => (f.special === "inbox" ? "Inbox" : f.name.split("/").pop() ?? f.name);

// A drag of messages carries where they are and which.
export const DRAG = "application/x-mailer-messages";

export interface MailContext {
  me: Me;
  folders: Folder[];
  reloadFolders: () => Promise<void>;
  compose: (s: Start) => void;
}

// Mail is the frame: a header with the search bar, a sidebar with New
// message, the folders, flagged mail and the categories, then the list of
// messages beside the one being read or written.
export function Mail({ me, setMe, onSignOut }: { me: Me; setMe: (m: Me) => void; onSignOut: () => void }) {
  const [view, setView] = useState(readHash);
  const [folders, setFolders] = useState<Folder[]>([]);
  const [open, setOpen] = useState<Ref | null>(null);
  const [writing, setWriting] = useState<Start | null>(null);
  const [drawer, setDrawer] = useState(false);
  const [tick, setTick] = useState(0);
  const notify = useNotify();

  const reloadFolders = useCallback(async () => {
    try {
      setFolders(await api.folders());
    } catch {
      // A failed refresh keeps the last list; a lost session signs out.
    }
  }, []);

  useEffect(() => {
    reloadFolders();
    const t = setInterval(() => {
      reloadFolders();
      setTick((n) => n + 1);
    }, 30000);
    return () => clearInterval(t);
  }, [reloadFolders]);

  useEffect(() => {
    const onHash = () => {
      setView(readHash());
      setOpen(null);
      setDrawer(false);
    };
    addEventListener("hashchange", onHash);
    return () => removeEventListener("hashchange", onHash);
  }, []);

  const inbox = folders.find((f) => f.special === "inbox");
  useEffect(() => {
    document.title = inbox?.unread ? `Inbox (${inbox.unread}) - Mail` : "Mail";
  }, [inbox?.unread]);

  const changed = useCallback(() => {
    reloadFolders();
    setTick((n) => n + 1);
  }, [reloadFolders]);

  const ctx: MailContext = { me, folders, reloadFolders, compose: setWriting };

  // Messages dropped on a folder move there.
  const drop = async (e: DragEvent, to: string) => {
    e.preventDefault();
    (e.currentTarget as HTMLElement).classList.remove("drop");
    const data = e.dataTransfer.getData(DRAG);
    if (!data) return;
    const groups = JSON.parse(data) as Record<string, number[]>;
    try {
      for (const [folder, uids] of Object.entries(groups)) if (folder !== to) await api.move(folder, uids, to);
      notify(`Moved to ${to === "INBOX" ? "Inbox" : to}`);
      if (open && groups[open.folder]?.includes(open.uid)) setOpen(null);
      changed();
    } catch (err) {
      notify(message(err), "bad");
    }
  };

  const settings = view.kind === "settings";
  const section = view.kind === "settings" ? view.section : "";
  const reading = !settings && (writing !== null || open !== null);

  return (
    <div className="app mail-app">
      <header className="header">
        <div className="header-start">
          <button type="button" className="icon-btn menu-btn" aria-label="Menu" aria-expanded={drawer} onClick={() => setDrawer((d) => !d)}>
            <Icon name="menu" />
          </button>
          <a className="brand" href="#/"><span className="brand-mark"><Icon name="mail" size={20} /></span><span className="brand-name">Mail</span></a>
        </div>
        <Search view={view} />
        <div className="header-actions">
          <a className={"icon-btn" + (settings ? " on" : "")} href="#/settings" aria-label="Settings" title="Settings"><Icon name="settings" /></a>
          <MoreMenu label={`Signed in as ${me.email}`} icon="person" actions={[
            { label: me.email, icon: "person", disabled: true, onClick: () => {} },
            { label: "Settings", icon: "settings", onClick: () => go("#/settings") },
            { label: "Sign out", icon: "logout", onClick: onSignOut },
          ]} />
        </div>
      </header>

      <div className="shell">
        <nav className={"sidebar" + (drawer ? " open" : "")} aria-label="Folders">
          {settings ? (
            <>
              <a className="nav-back" href="#/"><Icon name="arrow_back" /><span>Mail</span></a>
              <hr className="nav-rule" />
              {SECTIONS.map(([key, label, icon], i) => (
                <a key={key} className={"nav-item quiet" + (section === key || (!section && i === 0) ? " on" : "")} href={`#/settings/${key}`}>
                  <Icon name={icon} /><span className="nav-label">{label}</span>
                </a>
              ))}
            </>
          ) : (
            <>
              <button type="button" className="compose-btn" disabled={!me.can_send} title={me.can_send ? undefined : "Sending is turned off for this account"}
                onClick={() => { setWriting({ mode: "new" }); setDrawer(false); }}>
                <Icon name="edit_square" size={22} />New message
              </button>
              {folders.map((f) => {
                const depth = f.name.split("/").length - 1;
                const on = view.kind === "folder" && view.folder === f.name;
                return (
                  <a key={f.name} className={"nav-item" + (on ? " on" : "")} href={folderHref(f.name)} style={depth ? { paddingLeft: 20 + depth * 16 } : undefined}
                    onDragOver={(e) => { if (e.dataTransfer.types.includes(DRAG)) { e.preventDefault(); e.currentTarget.classList.add("drop"); } }}
                    onDragLeave={(e) => e.currentTarget.classList.remove("drop")}
                    onDrop={(e) => drop(e, f.name)}>
                    <Icon name={folderIcon(f)} />
                    <span className="nav-label">{folderLabel(f)}</span>
                    {(f.special === "drafts" ? f.total : f.special === "sent" || f.special === "trash" ? 0 : f.unread) > 0 && (
                      <span className={"nav-count" + (f.special === "inbox" ? " alert" : "")}>{(f.special === "drafts" ? f.total : f.unread).toLocaleString()}</span>
                    )}
                  </a>
                );
              })}
              <NewFolder onMade={reloadFolders} />
              <hr className="nav-rule" />
              <a className={"nav-item" + (view.kind === "flagged" ? " on" : "")} href="#/flagged"><Icon name="flag" /><span className="nav-label">Flagged</span></a>
              {me.prefs.categories.length > 0 && <div className="nav-heading">Categories</div>}
              {me.prefs.categories.map((c) => (
                <a key={c.id} className={"nav-item" + (view.kind === "category" && view.id === c.id ? " on" : "")} href={"#/c/" + c.id}>
                  <span className="cat-dot" style={{ background: c.color }} />
                  <span className="nav-label">{c.name}</span>
                </a>
              ))}
            </>
          )}
        </nav>
        {drawer && <div className="scrim" onClick={() => setDrawer(false)} />}

        {settings ? (
          <main className="page">
            <Settings section={section} me={me} setMe={setMe} folders={folders} reloadFolders={reloadFolders} onSignOut={onSignOut} />
          </main>
        ) : (
          <main className={"mailbox" + (reading ? " reading" : "")}>
            <List key={viewKey(view)} view={view} ctx={ctx} open={open} tick={tick} onChanged={changed}
              onOpen={(ref, draft) => {
                if (draft) {
                  setOpen(null);
                  setWriting({ mode: "draft", ref });
                } else {
                  setWriting(null);
                  setOpen(ref);
                }
              }} />
            <section className="pane" aria-label={writing ? "New message" : "Message"}>
              {writing ? (
                <Compose key={JSON.stringify(writing)} start={writing} ctx={ctx}
                  onClose={(sent) => {
                    setWriting(null);
                    if (sent) changed();
                    else reloadFolders();
                  }} />
              ) : open ? (
                <Reader key={open.folder + open.uid} at={open} ctx={ctx} onClose={() => setOpen(null)} onChanged={changed}
                  onGone={() => { setOpen(null); changed(); }} />
              ) : (
                <div className="pane-empty">
                  <Icon name="mail" size={48} />
                  <p>Pick a message to read it here.</p>
                </div>
              )}
            </section>
          </main>
        )}
      </div>
    </div>
  );
}

// NewFolder adds a folder from the sidebar; a / in the name puts it inside another.
function NewFolder({ onMade }: { onMade: () => Promise<void> }) {
  const [editing, setEditing] = useState(false);
  const [name, setName] = useState("");
  const input = useRef<HTMLInputElement>(null);
  const notify = useNotify();
  useEffect(() => { if (editing) input.current?.focus(); }, [editing]);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return setEditing(false);
    try {
      await api.createFolder(name.trim());
      notify("Folder made");
      setName("");
      setEditing(false);
      await onMade();
    } catch (err) {
      notify(message(err), "bad");
    }
  };
  if (!editing) {
    return <button type="button" className="nav-item quiet nav-button" onClick={() => setEditing(true)}><Icon name="create_new_folder" /><span className="nav-label">New folder</span></button>;
  }
  return (
    <form className="nav-form" onSubmit={submit}>
      <input ref={input} className="input" placeholder="Folder name" aria-label="Folder name" value={name} onChange={(e) => setName(e.target.value)}
        onKeyDown={(e) => e.key === "Escape" && setEditing(false)} onBlur={() => !name.trim() && setEditing(false)} />
    </form>
  );
}

// Search looks through every folder; Ctrl+K focuses it.
function Search({ view }: { view: View }) {
  const [q, setQ] = useState(view.kind === "search" ? view.q : "");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (view.kind !== "search") setQ("");
  }, [view]);
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
  return (
    <form className="search" role="search" onSubmit={(e) => {
      e.preventDefault();
      go(q.trim() ? "#/search/" + encodeURIComponent(q.trim()) : "#/");
    }}>
      <Icon name="search" className="search-icon" />
      <input ref={input} type="search" aria-label="Search mail" placeholder="Search mail" spellCheck={false} autoComplete="off"
        data-1p-ignore data-lpignore="true" value={q} onChange={(e) => setQ(e.target.value)}
        onKeyDown={(e) => e.key === "Escape" && input.current?.blur()} />
      <kbd>Ctrl+K</kbd>
    </form>
  );
}
