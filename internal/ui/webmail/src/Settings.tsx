import { useEffect, useRef, useState, type ClipboardEvent } from "react";
import { Appearance } from "../../shared/Appearance";
import { Dropdown, type Option } from "../../shared/Dropdown";
import { fmtSize, message, plural } from "../../shared/format";
import { Icon, type IconName } from "../../shared/Icon";
import { ArmedButton, Callout, Card, Row, SaveField, SaveSwitch, Switch, TextField, useNotify } from "../../shared/ui";
import { api } from "./api";
import { clean, textToHTML } from "./html";
import { folderIcon, folderLabel } from "./Mail";
import type { Action, Category, Condition, Field, Folder, Kind, Me, Op, Prefs, Rule, Signature } from "./types";

export const SECTIONS: [string, string, IconName][] = [
  ["general", "General", "tune"],
  ["signatures", "Signatures", "signature"],
  ["categories", "Categories", "label"],
  ["rules", "Rules", "rule"],
  ["folders", "Folders", "folder"],
  ["appearance", "Appearance", "brightness_auto"],
  ["account", "Account", "person"],
];

// A short id for something new, unique enough within one account's settings.
const newId = () => Math.random().toString(36).slice(2, 10);

export function Settings({ section, me, setMe, folders, reloadFolders, onSignOut }: {
  section: string;
  me: Me;
  setMe: (m: Me) => void;
  folders: Folder[];
  reloadFolders: () => Promise<void>;
  onSignOut: () => void;
}) {
  const [key, title] = SECTIONS.find(([k]) => k === section) ?? SECTIONS[0];
  // put saves the settings; what the server keeps is what's shown after.
  const put = async (patch: Partial<Prefs>) => {
    const prefs = await api.savePrefs({ ...me.prefs, ...patch });
    setMe({ ...me, prefs });
  };
  return (
    <>
      <div className="page-head">
        <div className="page-head-main">
          <h1>Settings</h1>
          <div className="page-sub">Saved as you change them, and kept with your account in every browser</div>
        </div>
      </div>
      <div className="page-body stack flow" key={key}>
        <h2 className="doc-title">{title}</h2>
        {key === "general" && <General me={me} put={put} />}
        {key === "signatures" && <Signatures prefs={me.prefs} put={put} />}
        {key === "categories" && <Categories prefs={me.prefs} put={put} />}
        {key === "rules" && <Rules prefs={me.prefs} folders={folders} put={put} />}
        {key === "folders" && <Folders folders={folders} reload={reloadFolders} />}
        {key === "appearance" && <Appearance />}
        {key === "account" && (
          <>
            <Card title="Signed in" sub={<>As <b>{me.email}</b>.</>}>
              {me.addresses.length > 1 && <Row title="Also sends as" sub={me.addresses.slice(1).join(", ")} />}
              <Row title="Sign out" sub="Ends this browser's session.">
                <button type="button" className="btn" onClick={onSignOut}><Icon name="logout" size={18} />Sign out</button>
              </Row>
            </Card>
            <Card title="Mail apps" sub="Outlook, Thunderbird and phone mail apps, beside this webmail.">
              <Row title={me.mail_apps ? "On for this account" : "Off for this account"}
                sub={me.mail_apps ? "Add the account in a mail app as IMAP, with your address and password." : "Your admin turns them on in the admin dashboard."}>
                <span className={"chip " + (me.mail_apps ? "ok" : "")}>{me.mail_apps ? "On" : "Off"}</span>
              </Row>
            </Card>
          </>
        )}
      </div>
    </>
  );
}

type Put = (patch: Partial<Prefs>) => Promise<void>;

function General({ me, put }: { me: Me; put: Put }) {
  return (
    <Card title="Reading and writing">
      <Row title="Your name" sub="What people see beside your address on what you send.">
        <SaveField label="Your name" width={240} placeholder="None" value={me.prefs.name} onSave={(v) => put({ name: v.trim() })} />
      </Row>
      <Row title="Mark messages read when opened" sub="Off leaves them unread until you mark them.">
        <SaveSwitch label="Mark messages read when opened" on={!me.prefs.keep_unread} onSave={(v) => put({ keep_unread: !v })} />
      </Row>
      <Row title="Show pictures from the internet" sub="Off hides them until you ask, so senders can't tell when you open their mail.">
        <SaveSwitch label="Show pictures from the internet" on={me.prefs.images} onSave={(v) => put({ images: v })} />
      </Row>
    </Card>
  );
}

// Editor is a small rich text box for a signature, cleaned like any message.
function Editor({ html, onChange }: { html: string; onChange: (html: string) => void }) {
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (box.current) box.current.innerHTML = clean(html);
    // Filled once; the editor is keyed by the signature it edits.
  }, []);
  const exec = (cmd: string) => {
    box.current?.focus();
    document.execCommand(cmd, false);
    onChange(box.current?.innerHTML ?? "");
  };
  const tools: [string, IconName, string][] = [["Bold", "format_bold", "bold"], ["Italic", "format_italic", "italic"], ["Underline", "format_underlined", "underline"]];
  return (
    <div className="sig-editor">
      <div className="editor-tools">
        {tools.map(([label, icon, cmd]) => (
          <button key={cmd} type="button" className="icon-btn small" aria-label={label} title={label} onMouseDown={(e) => e.preventDefault()} onClick={() => exec(cmd)}>
            <Icon name={icon} size={18} />
          </button>
        ))}
      </div>
      <div ref={box} className="editor small" contentEditable suppressContentEditableWarning role="textbox" aria-multiline="true" aria-label="Signature"
        onInput={() => onChange(box.current?.innerHTML ?? "")}
        onPaste={(e: ClipboardEvent) => {
          e.preventDefault();
          const h = e.clipboardData.getData("text/html");
          document.execCommand("insertHTML", false, h ? clean(h) : textToHTML(e.clipboardData.getData("text/plain")));
          onChange(box.current?.innerHTML ?? "");
        }} />
    </div>
  );
}

function Signatures({ prefs, put }: { prefs: Prefs; put: Put }) {
  const [editing, setEditing] = useState<Signature | null>(null);
  const notify = useNotify();
  const none: Option<string> = { value: "", label: "None" };
  const options = [none, ...prefs.signatures.map((s) => ({ value: s.id, label: s.name || "Signature" }))];
  const saveSig = async () => {
    if (!editing) return;
    const list = prefs.signatures.some((s) => s.id === editing.id)
      ? prefs.signatures.map((s) => (s.id === editing.id ? editing : s)) : [...prefs.signatures, editing];
    try {
      await put({ signatures: list, new_signature: prefs.new_signature || (prefs.signatures.length ? prefs.new_signature : editing.id) });
      setEditing(null);
      notify("Signature saved");
    } catch (e) {
      notify(message(e), "bad");
    }
  };
  const remove = (id: string) => put({
    signatures: prefs.signatures.filter((s) => s.id !== id),
    new_signature: prefs.new_signature === id ? "" : prefs.new_signature,
    reply_signature: prefs.reply_signature === id ? "" : prefs.reply_signature,
  }).catch((e) => notify(message(e), "bad"));

  return (
    <>
      <Card title="Used by default">
        <Row title="New messages"><Dropdown label="Signature on new messages" value={prefs.new_signature} options={options} onChange={(v) => put({ new_signature: v }).catch((e) => notify(message(e), "bad"))} /></Row>
        <Row title="Replies and forwards"><Dropdown label="Signature on replies" value={prefs.reply_signature} options={options} onChange={(v) => put({ reply_signature: v }).catch((e) => notify(message(e), "bad"))} /></Row>
      </Card>
      <Card title="Signatures" sub="Any one can also be put in from the writing toolbar." action={
        !editing && <button type="button" className="btn" onClick={() => setEditing({ id: newId(), name: "", html: "" })}><Icon name="add" size={18} />New signature</button>
      }>
        {editing && (
          <div className="card-pad edit-block" key={editing.id}>
            <TextField label="Name" autoFocus value={editing.name} onChange={(name) => setEditing({ ...editing, name })} />
            <Editor html={editing.html} onChange={(html) => setEditing({ ...editing, html })} />
            <div className="edit-actions">
              <button type="button" className="btn text" onClick={() => setEditing(null)}>Cancel</button>
              <button type="button" className="btn primary" disabled={!editing.name.trim()} onClick={saveSig}>Save</button>
            </div>
          </div>
        )}
        {prefs.signatures.length === 0 && !editing && <p className="empty-note">No signatures yet</p>}
        <ul className="item-list">
          {prefs.signatures.map((s) => (
            <li key={s.id} className="item-row">
              <Icon name="signature" size={18} className="item-icon" />
              <span className="person"><span className="scan-target">{s.name || "Signature"}</span>
                <small>{[s.id === prefs.new_signature && "New messages", s.id === prefs.reply_signature && "Replies"].filter(Boolean).join(", ") || "Not used by default"}</small></span>
              <button type="button" className="icon-btn small" aria-label={`Edit ${s.name}`} title="Edit" onClick={() => setEditing(s)}><Icon name="edit" size={18} /></button>
              <button type="button" className="icon-btn small danger" aria-label={`Remove ${s.name}`} title="Remove" onClick={() => remove(s.id)}><Icon name="close" size={18} /></button>
            </li>
          ))}
        </ul>
      </Card>
    </>
  );
}

// The colours a category can take.
const COLORS = ["#d93025", "#e37400", "#f9ab00", "#188038", "#12b5cb", "#1a73e8", "#9334e6", "#e52592", "#795548", "#5f6368"];

function Categories({ prefs, put }: { prefs: Prefs; put: Put }) {
  const [list, setList] = useState<Category[]>(prefs.categories);
  const notify = useNotify();
  useEffect(() => setList(prefs.categories), [prefs.categories]);
  const save = (next: Category[]) => {
    setList(next);
    return put({ categories: next }).catch((e) => {
      notify(message(e), "bad");
      setList(prefs.categories);
    });
  };
  const used = (id: string) => prefs.rules.some((r) => r.actions.some((a) => a.kind === "category" && a.value === id));
  return (
    <Card title="Categories" sub="Colour labels for messages, picked from the toolbar or put on by rules. Messages keep them in every mail app." action={
      <button type="button" className="btn" disabled={list.length >= 30} onClick={() => save([...list, { id: newId(), name: "New category", color: COLORS[list.length % COLORS.length] }])}>
        <Icon name="add" size={18} />New category
      </button>
    }>
      <ul className="item-list card-pad">
        {list.map((c, i) => (
          <li key={c.id} className="item-row cat-row">
            <span className="cat-dot big" style={{ background: c.color }} />
            <SaveField label="Category name" width={200} value={c.name} onSave={(name) => save(list.map((x, n) => (n === i ? { ...x, name: name.trim() || x.name } : x)))} />
            <span className="swatches" role="radiogroup" aria-label={`Colour of ${c.name}`}>
              {COLORS.map((color) => (
                <button key={color} type="button" role="radio" aria-checked={c.color === color} aria-label={color} className={"swatch" + (c.color === color ? " on" : "")}
                  style={{ background: color }} onClick={() => save(list.map((x, n) => (n === i ? { ...x, color } : x)))} />
              ))}
            </span>
            <button type="button" className="icon-btn small danger" aria-label={`Remove ${c.name}`} disabled={used(c.id)}
              title={used(c.id) ? "A rule puts this on; change the rule first" : "Remove"} onClick={() => save(list.filter((x) => x.id !== c.id))}>
              <Icon name="close" size={18} />
            </button>
          </li>
        ))}
      </ul>
    </Card>
  );
}

const FIELDS: Option<Field>[] = [
  { value: "from", label: "From" },
  { value: "to", label: "To or Cc" },
  { value: "subject", label: "Subject" },
  { value: "body", label: "Text" },
  { value: "anywhere", label: "Anywhere" },
  { value: "attachment", label: "Has an attachment" },
  { value: "marketing", label: "Is a newsletter or marketing" },
];
const OPS: Option<Op>[] = [
  { value: "contains", label: "contains" },
  { value: "not_contains", label: "doesn't contain" },
  { value: "is", label: "is" },
  { value: "starts", label: "starts with" },
  { value: "ends", label: "ends with" },
];
const KINDS: Option<Kind>[] = [
  { value: "move", label: "Move to" },
  { value: "copy", label: "Copy to" },
  { value: "category", label: "Mark with category" },
  { value: "read", label: "Mark read" },
  { value: "flag", label: "Flag it" },
  { value: "delete", label: "Delete it" },
];

const blankRule = (): Rule => ({
  id: newId(), name: "", on: true, any: false, stop: false,
  conditions: [{ field: "from", op: "contains", value: "" }],
  actions: [{ kind: "move", value: "" }],
});

const valueless = (f: Field) => f === "attachment" || f === "marketing";

function describe(r: Rule, prefs: Prefs) {
  const conds = r.conditions.map((c) => valueless(c.field) ? FIELDS.find((f) => f.value === c.field)?.label.toLowerCase()
    : `${FIELDS.find((f) => f.value === c.field)?.label.toLowerCase()} ${OPS.find((o) => o.value === c.op)?.label} "${c.value}"`);
  const acts = r.actions.map((a) => a.kind === "category" ? `mark ${prefs.categories.find((c) => c.id === a.value)?.name ?? "a category"}`
    : a.kind === "move" || a.kind === "copy" ? `${a.kind} to ${a.value}` : KINDS.find((k) => k.value === a.kind)?.label.toLowerCase());
  return `When ${conds.join(r.any ? " or " : " and ")}: ${acts.join(", ")}`;
}

// Rules file mail as it arrives, in order, as Outlook's do.
function Rules({ prefs, folders, put }: { prefs: Prefs; folders: Folder[]; put: Put }) {
  const [editing, setEditing] = useState<Rule | null>(null);
  const notify = useNotify();
  const save = async (rules: Rule[], done?: string) => {
    try {
      await put({ rules });
      if (done) notify(done);
      return true;
    } catch (e) {
      notify(message(e), "bad");
      return false;
    }
  };
  const moveRule = (i: number, by: number) => {
    const rules = [...prefs.rules];
    const [r] = rules.splice(i, 1);
    rules.splice(i + by, 0, r);
    save(rules);
  };
  const inbox = folders.find((f) => f.special === "inbox");
  return (
    <>
      <Card title="Rules" sub="Each new message is checked against them from the top; a rule set to stop keeps the ones under it from running on what it matched." action={
        !editing && <button type="button" className="btn" disabled={prefs.rules.length >= 100} onClick={() => setEditing(blankRule())}><Icon name="add" size={18} />New rule</button>
      }>
        {editing && (
          <RuleForm key={editing.id} rule={editing} prefs={prefs} folders={folders} onCancel={() => setEditing(null)} onSave={async (r) => {
            const rules = prefs.rules.some((x) => x.id === r.id) ? prefs.rules.map((x) => (x.id === r.id ? r : x)) : [...prefs.rules, r];
            if (await save(rules, "Rule saved")) setEditing(null);
          }} />
        )}
        {prefs.rules.length === 0 && !editing && <p className="empty-note card-pad">No rules yet</p>}
        <ul className="item-list card-pad">
          {prefs.rules.map((r, i) => (
            <li key={r.id} className="item-row">
              <Switch label={`${r.name} on`} on={r.on} onChange={(on) => save(prefs.rules.map((x) => (x.id === r.id ? { ...x, on } : x)))} />
              <span className="person"><span className="scan-target">{r.name}</span><small>{describe(r, prefs)}{r.stop ? ", then stop" : ""}</small></span>
              <button type="button" className="icon-btn small" aria-label="Move up" title="Move up" disabled={i === 0} onClick={() => moveRule(i, -1)}><Icon name="expand_less" size={18} /></button>
              <button type="button" className="icon-btn small" aria-label="Move down" title="Move down" disabled={i === prefs.rules.length - 1} onClick={() => moveRule(i, 1)}><Icon name="expand_more" size={18} /></button>
              <button type="button" className="icon-btn small" aria-label={`Edit ${r.name}`} title="Edit" onClick={() => setEditing(r)}><Icon name="edit" size={18} /></button>
              <button type="button" className="icon-btn small danger" aria-label={`Remove ${r.name}`} title="Remove" onClick={() => save(prefs.rules.filter((x) => x.id !== r.id), "Rule removed")}><Icon name="close" size={18} /></button>
            </li>
          ))}
        </ul>
      </Card>
      {inbox && prefs.rules.length > 0 && (
        <Card title="Run now">
          <Row title="Run the rules on the Inbox" sub="Files what's there already, as if each message had just arrived.">
            <button type="button" className="btn" onClick={() => api.runRules(inbox.name).then((r) => notify(r.matched ? `Rules filed ${plural(r.matched, "message")}` : "No message matched a rule"))
              .catch((e) => notify(message(e), "bad"))}><Icon name="rule" size={18} />Run now</button>
          </Row>
        </Card>
      )}
    </>
  );
}

function RuleForm({ rule, prefs, folders, onSave, onCancel }: { rule: Rule; prefs: Prefs; folders: Folder[]; onSave: (r: Rule) => void; onCancel: () => void }) {
  const [r, setR] = useState<Rule>(rule);
  const setCond = (i: number, c: Partial<Condition>) => setR({ ...r, conditions: r.conditions.map((x, n) => (n === i ? { ...x, ...c } : x)) });
  const setAct = (i: number, a: Partial<Action>) => setR({ ...r, actions: r.actions.map((x, n) => (n === i ? { ...x, ...a } : x)) });
  const folderOptions: Option<string>[] = [{ value: "", label: "Pick a folder" }, ...folders.map((f) => ({ value: f.name, label: f.special === "inbox" ? "Inbox" : f.name, icon: folderIcon(f) }))];
  const catOptions: Option<string>[] = [{ value: "", label: "Pick a category" }, ...prefs.categories.map((c) => ({ value: c.id, label: c.name }))];
  const ready = r.name.trim() !== "" && r.conditions.length > 0 && r.actions.length > 0
    && r.conditions.every((c) => valueless(c.field) || c.value.trim() !== "")
    && r.actions.every((a) => (a.kind === "move" || a.kind === "copy" || a.kind === "category" ? a.value !== "" : true));
  return (
    <div className="card-pad rule-form">
      <TextField label="Rule name" autoFocus value={r.name} onChange={(name) => setR({ ...r, name })} />
      <div className="rule-section">
        <span className="section-title">When</span>
        <Dropdown label="Match" value={r.any ? "any" : "all"} onChange={(v) => setR({ ...r, any: v === "any" })}
          options={[{ value: "all", label: "all of these match" }, { value: "any", label: "any one matches" }]} />
      </div>
      {r.conditions.map((c, i) => (
        <div key={i} className="rule-line">
          <Dropdown label="Field" value={c.field} options={FIELDS} onChange={(field) => setCond(i, { field })} />
          {!valueless(c.field) && <Dropdown label="Test" value={c.op} options={OPS} onChange={(op) => setCond(i, { op })} />}
          {!valueless(c.field) && <input className="input grow" aria-label="Value" value={c.value} onChange={(e) => setCond(i, { value: e.target.value })} />}
          <button type="button" className="icon-btn small" aria-label="Remove this test" title="Remove" disabled={r.conditions.length === 1}
            onClick={() => setR({ ...r, conditions: r.conditions.filter((_, n) => n !== i) })}><Icon name="close" size={18} /></button>
        </div>
      ))}
      <button type="button" className="btn text small" disabled={r.conditions.length >= 10}
        onClick={() => setR({ ...r, conditions: [...r.conditions, { field: "subject", op: "contains", value: "" }] })}><Icon name="add" size={18} />Another test</button>
      <div className="rule-section"><span className="section-title">Then</span></div>
      {r.actions.map((a, i) => (
        <div key={i} className="rule-line">
          <Dropdown label="Action" value={a.kind} options={KINDS} onChange={(kind) => setAct(i, { kind, value: "" })} />
          {(a.kind === "move" || a.kind === "copy") && <Dropdown label="Folder" value={a.value} options={folderOptions} onChange={(value) => setAct(i, { value })} />}
          {a.kind === "category" && <Dropdown label="Category" value={a.value} options={catOptions} onChange={(value) => setAct(i, { value })} />}
          <span className="grow" />
          <button type="button" className="icon-btn small" aria-label="Remove this action" title="Remove" disabled={r.actions.length === 1}
            onClick={() => setR({ ...r, actions: r.actions.filter((_, n) => n !== i) })}><Icon name="close" size={18} /></button>
        </div>
      ))}
      <button type="button" className="btn text small" disabled={r.actions.length >= 10}
        onClick={() => setR({ ...r, actions: [...r.actions, { kind: "read", value: "" }] })}><Icon name="add" size={18} />Another action</button>
      <Row title="Stop here" sub="Rules under this one don't run on what it matched.">
        <Switch label="Stop here" on={r.stop} onChange={(stop) => setR({ ...r, stop })} />
      </Row>
      <div className="edit-actions">
        <button type="button" className="btn text" onClick={onCancel}>Cancel</button>
        <button type="button" className="btn primary" disabled={!ready} onClick={() => onSave({ ...r, name: r.name.trim() })}>Save rule</button>
      </div>
    </div>
  );
}

function Folders({ folders, reload }: { folders: Folder[]; reload: () => Promise<void> }) {
  const [renaming, setRenaming] = useState("");
  const [name, setName] = useState("");
  const notify = useNotify();
  const act = async (p: Promise<unknown>, done: string) => {
    try {
      await p;
      notify(done);
      setRenaming("");
      await reload();
    } catch (e) {
      notify(message(e), "bad");
    }
  };
  return (
    <Card title="Folders" sub="Inbox, Drafts, Sent, Archive, Junk and Trash keep their names. A / in a name puts a folder inside another, such as Work/Clients.">
      <ul className="item-list card-pad">
        {folders.map((f) => (
          <li key={f.name} className="item-row">
            <Icon name={folderIcon(f)} size={18} className="item-icon" />
            {renaming === f.name ? (
              <form className="inline-edit grow" onSubmit={(e) => { e.preventDefault(); act(api.renameFolder(f.name, name.trim()), "Folder renamed"); }}>
                <input className="input grow" autoFocus aria-label="New name" value={name} onChange={(e) => setName(e.target.value)} />
                <button type="button" className="btn text small" onClick={() => setRenaming("")}>Cancel</button>
                <button className="btn primary small" disabled={!name.trim() || name.trim() === f.name}>Rename</button>
              </form>
            ) : (
              <span className="person"><span className="scan-target">{folderLabel(f)}</span><small>{plural(f.total, "message")}, {fmtSize(f.size)}{f.name.includes("/") ? `, in ${f.name.split("/").slice(0, -1).join("/")}` : ""}</small></span>
            )}
            {!f.special && renaming !== f.name && (
              <>
                <button type="button" className="icon-btn small" aria-label={`Rename ${f.name}`} title="Rename" onClick={() => { setRenaming(f.name); setName(f.name); }}><Icon name="edit" size={18} /></button>
                <ArmedButton small icon="delete" label="Remove" armedLabel="Its mail goes to Trash" onConfirm={() => act(api.deleteFolder(f.name), "Folder removed")} />
              </>
            )}
            {(f.special === "trash" || f.special === "junk") && f.total > 0 && (
              <ArmedButton small icon="delete" label="Empty" armedLabel="Delete for good" onConfirm={() => act(api.emptyFolder(f.name), `${folderLabel(f)} emptied`)} />
            )}
          </li>
        ))}
      </ul>
      <Callout icon="info">New folders are made from the sidebar, under the folder list. Messages move by dragging them onto a folder.</Callout>
    </Card>
  );
}
