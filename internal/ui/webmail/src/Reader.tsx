import { useEffect, useState, type ReactNode } from "react";
import { fmtSize, message } from "../../shared/format";
import { Icon } from "../../shared/Icon";
import { Callout, MoreMenu, useNotify, type MenuAction } from "../../shared/ui";
import { api, msgPath } from "./api";
import { addrList, hue, initials, longDate, URL_RE, who } from "./html";
import { folderIcon, folderLabel, type MailContext } from "./Mail";
import type { Detail, Ref } from "./types";

// Reader shows one message: who it's from and to, how its sender checked
// out, its text (HTML in a sandboxed frame, pictures from the internet only
// when asked), its attachments, and what can be done with it.
export function Reader({ at, ctx, onClose, onChanged, onGone }: {
  at: Ref;
  ctx: MailContext;
  onClose: () => void;
  onChanged: () => void;
  onGone: () => void;
}) {
  const { me, folders } = ctx;
  const [d, setD] = useState<Detail | null>(null);
  const [problem, setProblem] = useState("");
  const [images, setImages] = useState(me.prefs.images);
  const notify = useNotify();

  useEffect(() => {
    let live = true;
    api.message(at).then(async (m) => {
      if (!live) return;
      setD(m);
      if (!m.seen && !me.prefs.keep_unread) {
        await api.flags(at.folder, [at.uid], ["seen"]).catch(() => {});
        setD({ ...m, seen: true });
        onChanged();
      }
    }).catch((e) => live && setProblem(message(e)));
    return () => { live = false; };
    // Opened once per message; the reader is keyed by it.
  }, []);

  if (problem) return <div className="pane-empty"><Icon name="error" size={40} /><p>{problem}</p></div>;
  if (!d) return <div className="pane-empty"><span className="spinner" /></div>;

  const folder = folders.find((f) => f.name === d.folder);
  const trash = folders.find((f) => f.special === "trash");
  const junk = folders.find((f) => f.special === "junk");
  const inJunk = folder?.special === "junk";

  const run = async (fn: () => Promise<unknown>, done: string, gone = false) => {
    try {
      await fn();
      notify(done);
      if (gone) onGone();
      else onChanged();
    } catch (e) {
      notify(message(e), "bad");
    }
  };
  const setFlag = (name: string, on: boolean, done: string) =>
    run(() => (on ? api.flags(d.folder, [d.uid], [name]) : api.flags(d.folder, [d.uid], [], [name])), done).then(() => {
      if (name === "flagged") setD({ ...d, flagged: on });
      else if (name === "seen") setD({ ...d, seen: on });
      else if (name.startsWith("cat:")) {
        const id = name.slice(4);
        setD({ ...d, categories: on ? [...d.categories, id] : d.categories.filter((c) => c !== id) });
      }
    });

  const categoryActions: MenuAction[] = me.prefs.categories.map((c) => {
    const has = d.categories.includes(c.id);
    return { label: (has ? "Remove " : "") + c.name, icon: has ? "label_off" : "label", onClick: () => setFlag("cat:" + c.id, !has, has ? `${c.name} removed` : `Marked ${c.name}`) };
  });
  const moveActions: MenuAction[] = folders.filter((f) => f.name !== d.folder).map((f) => ({
    label: f.special === "inbox" ? "Inbox" : f.name, icon: folderIcon(f),
    onClick: () => run(() => api.move(d.folder, [d.uid], f.name), `Moved to ${folderLabel(f)}`, true),
  }));
  const ref = { folder: d.folder, uid: d.uid };
  const checks = d.auth.filter((a) => a.check !== "spf" || !d.auth.some((x) => x.check === "dmarc"));
  const passed = d.auth.length > 0 && d.auth.every((a) => a.result === "pass");

  return (
    <article className="reader">
      <div className="reader-tools">
        <button type="button" className="icon-btn small back-btn" aria-label="Back to the list" title="Back" onClick={onClose}><Icon name="arrow_back" size={18} /></button>
        <button type="button" className="btn small" disabled={!me.can_send} onClick={() => ctx.compose({ mode: "reply", ref })}><Icon name="reply" size={18} />Reply</button>
        <button type="button" className="btn small" disabled={!me.can_send} onClick={() => ctx.compose({ mode: "all", ref })}><Icon name="reply_all" size={18} />Reply all</button>
        <button type="button" className="btn small" disabled={!me.can_send} onClick={() => ctx.compose({ mode: "forward", ref })}><Icon name="forward" size={18} />Forward</button>
        <span className="grow" />
        <button type="button" className={"icon-btn small" + (d.flagged ? " flag-on" : "")} aria-pressed={d.flagged} aria-label={d.flagged ? "Clear flag" : "Flag"} title={d.flagged ? "Clear flag" : "Flag"}
          onClick={() => setFlag("flagged", !d.flagged, d.flagged ? "Flag cleared" : "Flagged")}><Icon name="flag" size={18} /></button>
        {categoryActions.length > 0 && <MoreMenu small label="Category" icon="label" actions={categoryActions} />}
        <MoreMenu small label="Move to" icon="drive_file_move" actions={moveActions} />
        {junk && (
          <button type="button" className="icon-btn small" aria-label={inJunk ? "Not junk" : "Junk"} title={inJunk ? "Not junk" : "Junk"}
            onClick={() => run(() => api.move(d.folder, [d.uid], inJunk ? "INBOX" : junk.name), inJunk ? "Moved to Inbox" : "Moved to Junk", true)}>
            <Icon name={inJunk ? "move_to_inbox" : "report"} size={18} />
          </button>
        )}
        <button type="button" className="icon-btn small danger" aria-label="Delete" title={folder?.special === "trash" ? "Delete for good" : "Delete"}
          onClick={() => run(() => api.remove(d.folder, [d.uid]), folder?.special === "trash" || !trash ? "Deleted for good" : "Moved to Trash", true)}>
          <Icon name="delete" size={18} />
        </button>
        <MoreMenu small label="More" actions={[
          { label: "Mark unread", icon: "mark_email_unread", onClick: () => setFlag("seen", false, "Marked unread").then(onClose) },
          { label: "Download message", icon: "download", onClick: () => { location.href = msgPath("raw", ref); } },
        ]} />
      </div>

      <div className="reader-scroll">
        <h1 className="reader-subject">{d.subject || "No subject"}</h1>
        {d.categories.length > 0 && (
          <div className="mail-tags">
            {d.categories.map((id) => {
              const c = me.prefs.categories.find((x) => x.id === id);
              return c && <span key={id} className="cat-chip" style={{ color: c.color }}>{c.name}</span>;
            })}
          </div>
        )}
        <div className="reader-from">
          <span className="avatar big" style={{ backgroundColor: `hsl(${hue(d.from.email || who(d.from))} 45% 42%)` }} aria-hidden>{initials(d.from)}</span>
          <div className="reader-people">
            <div><b>{who(d.from) || "Unknown sender"}</b>{d.from.name && <span className="muted"> {d.from.email}</span>}</div>
            {d.to.length > 0 && <div className="muted small-line">To {addrList(d.to)}</div>}
            {!!d.cc?.length && <div className="muted small-line">Cc {addrList(d.cc)}</div>}
            {!!d.bcc?.length && <div className="muted small-line">Bcc {addrList(d.bcc)}</div>}
          </div>
          <div className="reader-when">
            <span className="muted">{longDate(d.date)}</span>
            {d.auth.length > 0 && (
              <span className={"chip " + (passed ? "ok" : "warn")} title={d.auth.map((a) => `${a.check.toUpperCase()} ${a.result}`).join(", ")}>
                <Icon name={passed ? "verified" : "warning"} size={14} />
                {passed ? "Sender checked" : checks.map((a) => `${a.check.toUpperCase()} ${a.result}`).join(", ")}
              </span>
            )}
          </div>
        </div>

        {d.junk && <Callout tone="warn" icon="report">Filed as junk: {d.junk}.</Callout>}
        {d.risky && <Callout tone="bad" icon="warning">An attachment here can run code when opened. Open it only if you know the sender meant to send it.</Callout>}
        {d.html && d.remote && !images && (
          <Callout icon="image" action={<button type="button" className="btn small" onClick={() => setImages(true)}>Show pictures</button>}>
            Pictures from the internet are hidden, so the sender can't see you opened this.
          </Callout>
        )}

        {d.files.length > 0 && (
          <ul className="files">
            {d.files.map((f) => (
              <li key={f.index}>
                <a className="file" href={msgPath("part", ref, f.type.startsWith("image/") && f.type !== "image/svg+xml" ? { index: f.index, inline: 1 } : { index: f.index })}
                  target={f.type.startsWith("image/") ? "_blank" : undefined} rel="noopener noreferrer" download={f.type.startsWith("image/") ? undefined : f.name}>
                  <Icon name={f.type.startsWith("image/") ? "image" : "attach_file"} size={20} />
                  <span className="file-name">{f.name}</span>
                  <span className="muted">{fmtSize(f.size)}</span>
                </a>
              </li>
            ))}
          </ul>
        )}

        {d.html ? (
          <iframe className="reader-frame" title="Message" sandbox="allow-popups allow-popups-to-escape-sandbox"
            src={msgPath("body", ref, images ? { images: 1 } : {})} />
        ) : (
          <div className="reader-text">{linked(d.text)}</div>
        )}
      </div>
    </article>
  );
}

// linked is plain text with its web addresses as links.
function linked(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(URL_RE)) {
    const at = m.index ?? 0;
    if (at > last) out.push(text.slice(last, at));
    out.push(<a key={at} href={m[0]} target="_blank" rel="noopener noreferrer">{m[0]}</a>);
    last = at + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}
