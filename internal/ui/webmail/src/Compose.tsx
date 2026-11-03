import { useEffect, useRef, useState, type ClipboardEvent, type DragEvent, type KeyboardEvent, type ReactNode } from "react";
import { Dropdown } from "../../shared/Dropdown";
import { fmtSize, message } from "../../shared/format";
import { Icon, type IconName } from "../../shared/Icon";
import { Callout, MoreMenu, useNotify } from "../../shared/ui";
import { api } from "./api";
import { addrText, clean, escape, quoted, signatureHTML, subjectFor, textToHTML } from "./html";
import type { MailContext } from "./Mail";
import type { Addr, Detail, Draft, NewFile, Ref } from "./types";

export type Start =
  | { mode: "new"; to?: string }
  | { mode: "reply" | "all" | "forward" | "draft"; ref: Ref };

// A file that goes with the message: added here, or kept from the message
// being forwarded or the draft being finished (part is its index there).
interface Item {
  name: string;
  size: number;
  file?: NewFile;
  part?: number;
}

const ADDRESS = /^[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+$/;

// valid reports whether a typed recipient is an address, bare or with a name.
const valid = (v: string) => ADDRESS.test(v.trim()) || /<\s*[^\s@<>]+@[^\s@<>]+\.[^\s@<>]+\s*>$/.test(v.trim());

let contacts: Promise<Addr[]> | null = null;
const loadContacts = () => (contacts ??= api.contacts().catch(() => []));

function readFile(f: File): Promise<NewFile> {
  return new Promise((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve({ name: f.name, type: f.type || "application/octet-stream", data: String(r.result).replace(/^data:[^,]*,/, "") });
    r.onerror = () => reject(new Error(`${f.name} couldn't be read`));
    r.readAsDataURL(f);
  });
}

// Compose writes a message: new, a reply, a forward, or a saved draft
// carried on. It saves itself to Drafts every minute while it changes, and
// when it's closed.
export function Compose({ start, ctx, onClose }: { start: Start; ctx: MailContext; onClose: (sent: boolean) => void }) {
  const { me } = ctx;
  const prefs = me.prefs;
  const notify = useNotify();
  const editor = useRef<HTMLDivElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const [ready, setReady] = useState(false);
  const [from, setFrom] = useState(me.addresses[0] ?? me.email);
  const [to, setTo] = useState<string[]>(start.mode === "new" && start.to ? [start.to] : []);
  const [cc, setCc] = useState<string[]>([]);
  const [bcc, setBcc] = useState<string[]>([]);
  const [showCc, setShowCc] = useState(false);
  const [subject, setSubject] = useState("");
  const [items, setItems] = useState<Item[]>([]);
  const [keepFrom, setKeepFrom] = useState<Ref | null>(null);
  const [thread, setThread] = useState({ inReplyTo: "", references: "" });
  const [draft, setDraft] = useState<Ref | null>(null);
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState("");
  const [link, setLink] = useState<{ url: string; range: Range } | null>(null);
  const [problem, setProblem] = useState("");

  const sig = (id: string) => {
    const s = prefs.signatures.find((x) => x.id === id);
    return s ? "<p><br></p>" + signatureHTML(s.html) : "";
  };

  // Fill the fields from the message being answered, forwarded or finished.
  useEffect(() => {
    let live = true;
    const fill = (html: string) => {
      if (editor.current) editor.current.innerHTML = html;
      setReady(true);
    };
    if (start.mode === "new") {
      fill("<p><br></p>" + sig(prefs.new_signature));
      return;
    }
    const ref = start.ref;
    api.message(ref, true).then((d: Detail) => {
      if (!live) return;
      const mine = new Set(me.addresses);
      const strip = (list: Addr[] | null | undefined) => (list ?? []).filter((a) => !mine.has(a.email)).map(addrText);
      if (start.mode === "draft") {
        if (mine.has(d.from.email)) setFrom(d.from.email);
        setTo((d.to ?? []).map(addrText));
        setCc((d.cc ?? []).map(addrText));
        setBcc((d.bcc ?? []).map(addrText));
        setShowCc(!!(d.cc?.length || d.bcc?.length));
        setSubject(d.subject);
        setThread({ inReplyTo: d.in_reply_to, references: d.references });
        setItems(d.files.map((f) => ({ name: f.name, size: f.size, part: f.index })));
        setKeepFrom(ref);
        setDraft(ref);
        fill(d.html && d.source ? clean(d.source) : textToHTML(d.text));
        return;
      }
      const forward = start.mode === "forward";
      setSubject(subjectFor(d.subject, forward));
      if (!forward) {
        const back = d.reply_to?.length ? d.reply_to : [d.from];
        const toList = strip(back).length ? strip(back) : back.map(addrText);
        if (start.mode === "all") {
          const rest = strip([...d.to, ...(d.cc ?? [])]).filter((a) => !toList.includes(a));
          setTo(toList);
          setCc(rest);
          setShowCc(rest.length > 0);
        } else {
          setTo(toList);
        }
        setThread({ inReplyTo: d.message_id, references: (d.references + " " + d.message_id).trim() });
        // Answer from the address it was sent to, when it's one of ours.
        const at = [...d.to, ...(d.cc ?? [])].find((a) => mine.has(a.email));
        if (at) setFrom(at.email);
      } else {
        setItems(d.files.map((f) => ({ name: f.name, size: f.size, part: f.index })));
        setKeepFrom(ref);
      }
      fill("<p><br></p>" + sig(prefs.reply_signature) + "<p><br></p>" + quoted(d, forward));
    }).catch((e) => live && setProblem(message(e)));
    return () => { live = false; };
    // Filled once; the composer is keyed by what it started from.
  }, []);

  const body = (): Draft => {
    const files = items.filter((i) => i.file).map((i) => i.file!);
    const parts = items.filter((i) => i.part !== undefined).map((i) => i.part!);
    const reply = start.mode === "reply" || start.mode === "all" || start.mode === "forward" ? start.ref : null;
    return {
      from, to, cc, bcc, subject,
      html: editor.current?.innerHTML ?? "",
      text: editor.current?.innerText ?? "",
      in_reply_to: thread.inReplyTo, references: thread.references,
      files,
      keep: keepFrom && parts.length ? { ...keepFrom, parts } : null,
      reply, forward: start.mode === "forward",
      replaces: draft,
    };
  };

  // save files the message in Drafts. Afterwards its attachments all live
  // in the new draft, after its two text parts.
  const save = async (quiet = false) => {
    setBusy("save");
    try {
      const ref = await api.saveDraft(body());
      setDraft(ref);
      setKeepFrom(ref);
      setItems((list) => list.map((i, n) => ({ name: i.name, size: i.size, part: n + 2 })));
      setDirty(false);
      if (!quiet) notify("Saved to Drafts");
      ctx.reloadFolders();
      return true;
    } catch (e) {
      notify(message(e), "bad");
      return false;
    } finally {
      setBusy("");
    }
  };

  useEffect(() => {
    if (!dirty) return;
    const t = setTimeout(() => save(true), 60000);
    return () => clearTimeout(t);
  }, [dirty, items, to, cc, bcc, subject]);

  const send = async () => {
    const all = [...to, ...cc, ...bcc];
    if (!all.length) return notify("Add someone to send it to", "bad");
    const bad = all.find((a) => !valid(a));
    if (bad) return notify(`${bad} isn't an email address`, "bad");
    setBusy("send");
    try {
      await api.send(body());
      notify("Sent");
      onClose(true);
    } catch (e) {
      notify(message(e), "bad");
      setBusy("");
    }
  };

  const close = async () => {
    if (dirty && !(await save(true))) return;
    if (dirty) notify("Saved to Drafts");
    onClose(false);
  };

  const discard = async () => {
    if (draft) await api.remove(draft.folder, [draft.uid]).catch(() => {});
    notify("Discarded");
    onClose(true);
  };

  const add = async (files: FileList | File[]) => {
    const list = Array.from(files);
    const total = items.reduce((n, i) => n + i.size, 0) + list.reduce((n, f) => n + f.size, 0);
    if (total > me.max_bytes * 0.95) return notify(`Attachments can add up to ${fmtSize(me.max_bytes)}`, "bad");
    try {
      const read = await Promise.all(list.map(readFile));
      setItems((prev) => [...prev, ...read.map((file, n) => ({ name: file.name, size: list[n].size, file }))]);
      setDirty(true);
    } catch (e) {
      notify(message(e), "bad");
    }
  };

  const exec = (cmd: string, value?: string) => {
    editor.current?.focus();
    document.execCommand(cmd, false, value);
    setDirty(true);
  };

  const onPaste = (e: ClipboardEvent) => {
    const html = e.clipboardData.getData("text/html");
    const text = e.clipboardData.getData("text/plain");
    if (!html && !text) return;
    e.preventDefault();
    document.execCommand("insertHTML", false, html ? clean(html) : textToHTML(text));
    setDirty(true);
  };

  const openLink = () => {
    const sel = getSelection();
    if (!sel || !sel.rangeCount || !editor.current?.contains(sel.anchorNode)) {
      editor.current?.focus();
      return;
    }
    setLink({ url: "https://", range: sel.getRangeAt(0).cloneRange() });
  };
  const applyLink = () => {
    if (!link) return;
    const url = link.url.trim();
    if (!/^(https?:\/\/|mailto:)\S+$/i.test(url)) return notify("Links start with https:// or mailto:", "bad");
    const sel = getSelection();
    sel?.removeAllRanges();
    sel?.addRange(link.range);
    if (link.range.collapsed) document.execCommand("insertHTML", false, `<a href="${escape(url)}">${escape(url)}</a>`);
    else document.execCommand("createLink", false, url);
    setLink(null);
    setDirty(true);
  };

  const tools: [string, IconName, () => void][] = [
    ["Bold", "format_bold", () => exec("bold")],
    ["Italic", "format_italic", () => exec("italic")],
    ["Underline", "format_underlined", () => exec("underline")],
    ["Bulleted list", "format_list_bulleted", () => exec("insertUnorderedList")],
    ["Numbered list", "format_list_numbered", () => exec("insertOrderedList")],
    ["Quote", "format_quote", () => exec("formatBlock", "blockquote")],
    ["Link", "link", openLink],
    ["Clear formatting", "format_clear", () => exec("removeFormat")],
  ];

  if (problem) return <div className="pane-empty"><Icon name="error" size={40} /><p>{problem}</p><button type="button" className="btn" onClick={() => onClose(false)}>Close</button></div>;

  return (
    <div className="compose" onDragOver={(e: DragEvent) => { if (e.dataTransfer.types.includes("Files")) e.preventDefault(); }}
      onDrop={(e: DragEvent) => { if (e.dataTransfer.files.length) { e.preventDefault(); add(e.dataTransfer.files); } }}>
      <div className="compose-head">
        <h2>{start.mode === "reply" || start.mode === "all" ? "Reply" : start.mode === "forward" ? "Forward" : start.mode === "draft" ? "Draft" : "New message"}</h2>
        <span className="muted">{busy === "save" ? "Saving" : draft && !dirty ? "Saved to Drafts" : ""}</span>
        <span className="grow" />
        <button type="button" className="icon-btn small" aria-label="Close and keep as a draft" title="Close" onClick={close}><Icon name="close" size={18} /></button>
      </div>
      {!me.can_send && <Callout tone="warn" icon="block">Sending is turned off for this account. You can still save drafts.</Callout>}
      <div className="compose-fields">
        {me.addresses.length > 1 && (
          <div className="field-row">
            <span className="field-label">From</span>
            <Dropdown label="From" value={from} onChange={setFrom} options={me.addresses.map((a) => ({ value: a, label: prefs.name ? `${prefs.name} <${a}>` : a }))} />
          </div>
        )}
        <Recipients label="To" value={to} onChange={(v) => { setTo(v); setDirty(true); }} autoFocus={start.mode === "new" || start.mode === "forward"}
          extra={!showCc && <button type="button" className="tf-action" onClick={() => setShowCc(true)}>Cc Bcc</button>} />
        {showCc && <Recipients label="Cc" value={cc} onChange={(v) => { setCc(v); setDirty(true); }} />}
        {showCc && <Recipients label="Bcc" value={bcc} onChange={(v) => { setBcc(v); setDirty(true); }} />}
        <div className="field-row">
          <span className="field-label">Subject</span>
          <input className="field-input" aria-label="Subject" value={subject} onChange={(e) => { setSubject(e.target.value); setDirty(true); }} />
        </div>
      </div>
      <div className="editor-tools" role="toolbar" aria-label="Formatting">
        {tools.map(([label, icon, fn]) => (
          <button key={label} type="button" className="icon-btn small" aria-label={label} title={label} onMouseDown={(e) => e.preventDefault()} onClick={fn}>
            <Icon name={icon} size={18} />
          </button>
        ))}
        {prefs.signatures.length > 0 && (
          <MoreMenu small label="Signature" icon="signature" actions={prefs.signatures.map((s) => ({
            label: s.name || "Signature", icon: "signature", onClick: () => exec("insertHTML", signatureHTML(s.html)),
          }))} />
        )}
        <button type="button" className="icon-btn small" aria-label="Attach files" title="Attach files" onClick={() => picker.current?.click()}><Icon name="attach_file" size={18} /></button>
        <input ref={picker} type="file" multiple hidden onChange={(e) => { if (e.target.files) add(e.target.files); e.target.value = ""; }} />
        {link && (
          <span className="link-edit">
            <input className="input" autoFocus aria-label="Link address" value={link.url} onChange={(e) => setLink({ ...link, url: e.target.value })}
              onKeyDown={(e: KeyboardEvent) => {
                if (e.key === "Enter") { e.preventDefault(); applyLink(); }
                if (e.key === "Escape") setLink(null);
              }} />
            <button type="button" className="btn small" onClick={applyLink}>Link</button>
          </span>
        )}
      </div>
      <div ref={editor} className={"editor" + (ready ? "" : " loading")} contentEditable={ready} suppressContentEditableWarning role="textbox" aria-multiline="true"
        aria-label="Message" spellCheck onInput={() => setDirty(true)} onPaste={onPaste} />
      {items.length > 0 && (
        <ul className="files">
          {items.map((f, n) => (
            <li key={n} className="file">
              <Icon name="attach_file" size={18} />
              <span className="file-name">{f.name}</span>
              <span className="muted">{fmtSize(f.size)}</span>
              <button type="button" className="icon-btn small" aria-label={`Remove ${f.name}`} title="Remove" onClick={() => { setItems(items.filter((_, i) => i !== n)); setDirty(true); }}>
                <Icon name="close" size={16} />
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="compose-foot">
        <button type="button" className="btn primary" disabled={!!busy || !me.can_send || !ready} onClick={send}>
          {busy === "send" ? <span className="spinner small on-accent" /> : <Icon name="send" size={18} />}Send
        </button>
        <button type="button" className="btn" disabled={!!busy || !ready} onClick={() => save()}><Icon name="draft" size={18} />Save draft</button>
        <span className="grow" />
        <button type="button" className="icon-btn danger" aria-label="Discard" title="Discard" disabled={!!busy} onClick={discard}><Icon name="delete" size={20} /></button>
      </div>
    </div>
  );
}

// Recipients is a row of addresses as chips, with suggestions from the people
// written to and heard from most.
function Recipients({ label, value, onChange, autoFocus, extra }: {
  label: string;
  value: string[];
  onChange: (v: string[]) => void;
  autoFocus?: boolean;
  extra?: ReactNode;
}) {
  const [text, setText] = useState("");
  const [people, setPeople] = useState<Addr[]>([]);
  const [active, setActive] = useState(0);
  const [focus, setFocus] = useState(false);
  useEffect(() => { loadContacts().then(setPeople); }, []);

  const needle = text.trim().toLowerCase();
  const matches = needle ? people.filter((p) => (p.email.includes(needle) || (p.name ?? "").toLowerCase().includes(needle)) && !value.includes(addrText(p))).slice(0, 6) : [];

  const commit = (v: string) => {
    const parts = v.split(/[,;]+/).map((s) => s.trim()).filter(Boolean);
    if (parts.length) onChange([...value, ...parts.filter((p) => !value.includes(p))]);
    setText("");
    setActive(0);
  };

  return (
    <div className="field-row recipients">
      <span className="field-label">{label}</span>
      <div className="chips-input" onClick={(e) => (e.currentTarget.querySelector("input") as HTMLInputElement | null)?.focus()}>
        {value.map((v) => (
          <span key={v} className={"addr-chip" + (valid(v) ? "" : " bad")} title={v}>
            {v.replace(/\s*<[^>]+>$/, "") || v}
            <button type="button" aria-label={`Remove ${v}`} onClick={() => onChange(value.filter((x) => x !== v))}><Icon name="close" size={14} /></button>
          </span>
        ))}
        <input className="field-input" aria-label={label} autoFocus={autoFocus} value={text} spellCheck={false} autoComplete="off"
          onFocus={() => setFocus(true)}
          onBlur={() => { setFocus(false); if (text.trim()) commit(text); }}
          onChange={(e) => {
            const v = e.target.value;
            if (/[,;]$/.test(v)) commit(v);
            else {
              setText(v);
              setActive(0);
            }
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === "Tab") {
              if (matches[active] && focus) {
                e.preventDefault();
                commit(addrText(matches[active]));
              } else if (text.trim()) {
                e.preventDefault();
                commit(text);
              }
            } else if (e.key === "Backspace" && !text && value.length) {
              onChange(value.slice(0, -1));
            } else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
              e.preventDefault();
              setActive((a) => Math.max(0, Math.min(matches.length - 1, a + (e.key === "ArrowDown" ? 1 : -1))));
            }
          }} />
        {focus && matches.length > 0 && (
          <ul className="suggest" role="listbox">
            {matches.map((p, i) => (
              <li key={p.email} role="option" aria-selected={i === active} className={"recent-item" + (i === active ? " active" : "")}
                onMouseDown={(e) => { e.preventDefault(); commit(addrText(p)); }} onMouseEnter={() => setActive(i)}>
                <span className="recent-target">{p.name || p.email}</span>
                {p.name && <span className="recent-when">{p.email}</span>}
              </li>
            ))}
          </ul>
        )}
      </div>
      {extra}
    </div>
  );
}
