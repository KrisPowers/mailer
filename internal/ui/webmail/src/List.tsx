import { useCallback, useEffect, useState } from "react";
import { message, plural } from "../../shared/format";
import { Icon } from "../../shared/Icon";
import { Filters, MoreMenu, useNotify, type MenuAction } from "../../shared/ui";
import { api, type ListQuery } from "./api";
import { hue, initials, listDate, who } from "./html";
import { DRAG, folderIcon, folderLabel, type MailContext } from "./Mail";
import type { View } from "./route";
import type { Ref, Summary } from "./types";

const PAGE = 50;

type Show = "all" | "unread" | "flagged" | "attachments";

const keyOf = (m: { folder: string; uid: number }) => m.folder + "\u0000" + m.uid;

// groups sorts messages by the folder they're in, for calls that take one
// folder at a time.
export function groups(list: { folder: string; uid: number }[]) {
  const out: Record<string, number[]> = {};
  for (const m of list) (out[m.folder] ??= []).push(m.uid);
  return out;
}

// List is the messages of one folder, of everything flagged, of one
// category, or of a search, newest first, with what can be done to them.
export function List({ view, ctx, open, tick, onChanged, onOpen }: {
  view: View;
  ctx: MailContext;
  open: Ref | null;
  tick: number;
  onChanged: () => void;
  onOpen: (ref: Ref, draft: boolean) => void;
}) {
  const { me, folders } = ctx;
  const [show, setShow] = useState<Show>("all");
  const [items, setItems] = useState<Summary[] | null>(null);
  const [total, setTotal] = useState(0);
  const [limit, setLimit] = useState(PAGE);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [busy, setBusy] = useState(false);
  const notify = useNotify();

  const folder = view.kind === "folder" ? folders.find((f) => f.name === view.folder) : undefined;
  const q: ListQuery = view.kind === "folder" ? { folder: view.folder, show: show === "all" ? "" : show }
    : view.kind === "flagged" ? { all: true, show: "flagged" }
    : view.kind === "category" ? { all: true, show: "cat:" + view.id }
    : view.kind === "search" ? { all: true, q: view.q }
    : {};
  const qKey = JSON.stringify(q);

  const load = useCallback(async () => {
    try {
      const page = await api.messages({ ...JSON.parse(qKey), offset: 0, limit });
      setItems(page.messages);
      setTotal(page.total);
      const here = new Set(page.messages.map(keyOf));
      setPicked((p) => new Set([...p].filter((k) => here.has(k))));
    } catch (e) {
      notify(message(e), "bad");
      setItems([]);
    }
  }, [qKey, limit, notify]);

  useEffect(() => { load(); }, [load, tick]);

  const title = view.kind === "folder" ? (folder ? folderLabel(folder) : view.folder)
    : view.kind === "flagged" ? "Flagged"
    : view.kind === "category" ? (me.prefs.categories.find((c) => c.id === view.id)?.name ?? "Category")
    : view.kind === "search" ? `Results for "${view.q}"` : "";
  const crossFolder = view.kind !== "folder";
  const outgoing = folder?.special === "sent" || folder?.special === "drafts";
  const list = items ?? [];
  const chosen = list.filter((m) => picked.has(keyOf(m)));
  const all = list.length > 0 && chosen.length === list.length;
  const trash = folders.find((f) => f.special === "trash");
  const junk = folders.find((f) => f.special === "junk");

  // act runs a call for each folder the chosen messages are in.
  const act = async (fn: (folder: string, uids: number[]) => Promise<unknown>, done: string, targets = chosen) => {
    setBusy(true);
    try {
      for (const [f, uids] of Object.entries(groups(targets))) await fn(f, uids);
      notify(done);
      setPicked(new Set());
      onChanged();
    } catch (e) {
      notify(message(e), "bad");
    } finally {
      setBusy(false);
    }
  };
  const flag = (m: Summary) => act((f, uids) => (m.flagged ? api.flags(f, uids, [], ["flagged"]) : api.flags(f, uids, ["flagged"])),
    m.flagged ? "Flag cleared" : "Flagged", [m]);

  const categoryActions: MenuAction[] = me.prefs.categories.map((c) => {
    const every = chosen.length > 0 && chosen.every((m) => m.categories.includes(c.id));
    return {
      label: (every ? "Remove " : "") + c.name, icon: every ? "label_off" : "label",
      onClick: () => act((f, uids) => (every ? api.flags(f, uids, [], ["cat:" + c.id]) : api.flags(f, uids, ["cat:" + c.id])), every ? `${c.name} removed` : `Marked ${c.name}`),
    };
  });
  const moveActions: MenuAction[] = folders.filter((f) => view.kind !== "folder" || f.name !== view.folder).map((f) => ({
    label: f.special === "inbox" ? "Inbox" : f.name, icon: folderIcon(f),
    onClick: () => act((from, uids) => api.move(from, uids, f.name), `Moved to ${folderLabel(f)}`),
  }));
  const inJunk = folder?.special === "junk";

  const runRules = async () => {
    if (!folder) return;
    try {
      const r = await api.runRules(folder.name);
      notify(r.matched ? `Rules filed ${plural(r.matched, "message")}` : "No message matched a rule");
      onChanged();
    } catch (e) {
      notify(message(e), "bad");
    }
  };

  const empty = async () => {
    if (!folder) return;
    try {
      await api.emptyFolder(folder.name);
      notify(`${folderLabel(folder)} emptied`);
      onChanged();
    } catch (e) {
      notify(message(e), "bad");
    }
  };

  const toggle = (m: Summary) => setPicked((p) => {
    const n = new Set(p);
    if (n.has(keyOf(m))) n.delete(keyOf(m));
    else n.add(keyOf(m));
    return n;
  });

  return (
    <section className="list" aria-label="Messages">
      <div className="list-head">
        <div className="list-title">
          <h1>{title}</h1>
          <span className="muted">{items ? plural(total, "message") : ""}{folder && folder.unread > 0 && `, ${folder.unread.toLocaleString()} unread`}</span>
        </div>
        {folder && (
          <MoreMenu label="Folder actions" actions={[
            { label: "Run rules on this folder", icon: "rule", onClick: runRules },
            { label: "Mark all read", icon: "mark_email_read", onClick: () => act((f, uids) => api.flags(f, uids, ["seen"]), "Marked read", list.filter((m) => !m.seen)) },
            ...(folder.special === "trash" || folder.special === "junk"
              ? [{ label: `Empty ${folderLabel(folder)}`, icon: "delete" as const, danger: true, confirm: "Delete everything for good", onClick: empty }] : []),
          ]} />
        )}
      </div>
      {view.kind === "folder" && (
        <Filters<Show> value={show} onChange={(v) => { setShow(v); setLimit(PAGE); }} items={[["all", "All"], ["unread", "Unread"], ["flagged", "Flagged"], ["attachments", "Attachments"]]} />
      )}
      <div className="list-tools">
        <button type="button" className="check" role="checkbox" aria-checked={all} aria-label="Pick all" title="Pick all"
          onClick={() => setPicked(all ? new Set() : new Set(list.map(keyOf)))}>
          <span className="check-box">{all ? <Icon name="check" size={16} /> : chosen.length > 0 && <span className="check-some" />}</span>
        </button>
        {chosen.length === 0 ? (
          <>
            <button type="button" className="icon-btn small" aria-label="Refresh" title="Refresh" onClick={() => { load(); ctx.reloadFolders(); }}><Icon name="refresh" size={18} /></button>
            <span className="muted list-hint">{list.length ? "Pick messages to act on several at once" : ""}</span>
          </>
        ) : (
          <>
            <span className="list-count">{chosen.length}</span>
            <button type="button" className="icon-btn small" disabled={busy} aria-label="Mark read" title="Mark read" onClick={() => act((f, uids) => api.flags(f, uids, ["seen"]), "Marked read")}><Icon name="mark_email_read" size={18} /></button>
            <button type="button" className="icon-btn small" disabled={busy} aria-label="Mark unread" title="Mark unread" onClick={() => act((f, uids) => api.flags(f, uids, [], ["seen"]), "Marked unread")}><Icon name="mark_email_unread" size={18} /></button>
            <button type="button" className="icon-btn small" disabled={busy} aria-label="Flag" title="Flag" onClick={() => act((f, uids) => api.flags(f, uids, ["flagged"]), "Flagged")}><Icon name="flag" size={18} /></button>
            {categoryActions.length > 0 && <MoreMenu small label="Category" icon="label" actions={categoryActions} />}
            <MoreMenu small label="Move to" icon="drive_file_move" actions={moveActions} />
            {junk && (
              <button type="button" className="icon-btn small" disabled={busy} aria-label={inJunk ? "Not junk" : "Junk"} title={inJunk ? "Not junk" : "Junk"}
                onClick={() => act((f, uids) => api.move(f, uids, inJunk ? "INBOX" : junk.name), inJunk ? "Moved to Inbox" : "Moved to Junk")}>
                <Icon name={inJunk ? "move_to_inbox" : "report"} size={18} />
              </button>
            )}
            <button type="button" className="icon-btn small danger" disabled={busy} aria-label="Delete" title={folder?.special === "trash" ? "Delete for good" : "Delete"}
              onClick={() => act((f, uids) => api.remove(f, uids), folder?.special === "trash" || !trash ? "Deleted for good" : "Moved to Trash")}>
              <Icon name="delete" size={18} />
            </button>
          </>
        )}
      </div>

      <ul className="mail-list">
        {items === null && <li className="list-note"><span className="spinner tiny" />Loading</li>}
        {items !== null && list.length === 0 && (
          <li className="list-note">
            <Icon name={view.kind === "search" ? "search" : folderIcon(folder)} size={40} />
            {view.kind === "search" ? "Nothing matches" : show !== "all" ? "Nothing here with that filter" : "Nothing here"}
          </li>
        )}
        {list.map((m) => {
          const k = keyOf(m);
          const isOpen = open?.folder === m.folder && open.uid === m.uid;
          const person = outgoing ? (m.to[0] ?? { email: "" }) : m.from;
          const name = outgoing ? (m.to.length ? "To " + m.to.map(who).join(", ") : "No recipients") : who(m.from) || "Unknown sender";
          return (
            <li key={k} className={"mail-row" + (m.seen ? "" : " unread") + (picked.has(k) ? " picked" : "") + (isOpen ? " open" : "")}
              draggable onDragStart={(e) => {
                const targets = picked.has(k) ? chosen : [m];
                e.dataTransfer.setData(DRAG, JSON.stringify(groups(targets)));
                e.dataTransfer.effectAllowed = "move";
              }}
              onClick={() => onOpen({ folder: m.folder, uid: m.uid }, m.draft && folders.find((f) => f.name === m.folder)?.special === "drafts")}>
              <button type="button" className="check" role="checkbox" aria-checked={picked.has(k)} aria-label="Pick"
                onClick={(e) => { e.stopPropagation(); toggle(m); }}>
                <span className="check-box">{picked.has(k) && <Icon name="check" size={16} />}</span>
              </button>
              <span className="avatar" style={{ backgroundColor: `hsl(${hue(person.email || name)} 45% 42%)` }} aria-hidden>{initials(person)}</span>
              <div className="mail-main">
                <div className="mail-top">
                  <span className="mail-who">{name}</span>
                  {m.attachments && <Icon name="attach_file" size={16} className="mail-mark" />}
                  {m.answered && <Icon name="reply" size={16} className="mail-mark" />}
                  {m.forwarded && <Icon name="forward" size={16} className="mail-mark" />}
                  <span className="mail-date">{listDate(m.date)}</span>
                </div>
                <div className="mail-subject">{m.draft && <span className="draft-word">Draft</span>}{m.subject || "No subject"}</div>
                <div className="mail-snippet">{m.snippet}</div>
                {(crossFolder || m.categories.length > 0 || m.risky) && (
                  <div className="mail-tags">
                    {crossFolder && <span className="chip">{m.folder === "INBOX" ? "Inbox" : m.folder}</span>}
                    {m.risky && <span className="chip bad">Risky file</span>}
                    {m.categories.map((id) => {
                      const c = me.prefs.categories.find((x) => x.id === id);
                      return c && <span key={id} className="cat-chip" style={{ color: c.color }}>{c.name}</span>;
                    })}
                  </div>
                )}
              </div>
              <button type="button" className={"flag-btn" + (m.flagged ? " on" : "")} aria-pressed={m.flagged} aria-label={m.flagged ? "Clear flag" : "Flag"} title={m.flagged ? "Clear flag" : "Flag"}
                onClick={(e) => { e.stopPropagation(); flag(m); }}>
                <Icon name="flag" size={18} />
              </button>
            </li>
          );
        })}
        {items !== null && list.length < total && (
          <li className="list-note"><button type="button" className="btn" onClick={() => setLimit((l) => l + PAGE)}>Load more</button></li>
        )}
      </ul>
    </section>
  );
}
