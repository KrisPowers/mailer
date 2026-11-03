import { createContext, useCallback, useContext, useEffect, useId, useLayoutEffect, useRef, useState, type FormEvent, type InputHTMLAttributes, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useDismiss } from "./dismiss";
import { generatePassword, message } from "./format";
import { Icon, type IconName } from "./Icon";

// PageHead is a page's heading: its title, a line of facts under it, and its
// actions on the right. With back, an arrow before the title goes there.
export function PageHead({ title, sub, actions, back, lead }: { title: ReactNode; sub?: ReactNode; actions?: ReactNode; back?: string; lead?: ReactNode }) {
  return (
    <div className="page-head">
      {back && <a className="icon-btn" href={back} aria-label="Back" title="Back"><Icon name="arrow_back" /></a>}
      <div className={"page-head-main" + (lead ? " person-head" : "")}>
        {lead}
        <div className="page-head-text">
          <h1>{title}</h1>
          {sub && <div className="page-sub">{sub}</div>}
        </div>
      </div>
      {actions && <div className="page-actions">{actions}</div>}
    </div>
  );
}

// Card is a soft filled block with a heading.
export function Card({ title, sub, action, children, className }: { title?: ReactNode; sub?: ReactNode; action?: ReactNode; children?: ReactNode; className?: string }) {
  return (
    <section className={"card" + (className ? " " + className : "")}>
      {(title || action) && (
        <div className="card-head">
          <div className="card-titles">
            {title && <h2>{title}</h2>}
            {sub && <p>{sub}</p>}
          </div>
          {action}
        </div>
      )}
      {children}
    </section>
  );
}

// Row is a setting: what it is on the left, its control on the right.
export function Row({ title, sub, children }: { title: ReactNode; sub?: ReactNode; children?: ReactNode }) {
  return (
    <div className="setting">
      <div className="setting-text">
        <div className="setting-title">{title}</div>
        {sub && <div className="setting-sub">{sub}</div>}
      </div>
      {children}
    </div>
  );
}

export function Switch({ label, on, onChange, disabled }: { label: string; on: boolean; onChange: (on: boolean) => void; disabled?: boolean }) {
  return (
    <button type="button" role="switch" aria-checked={on} aria-label={label} className="switch" disabled={disabled} onClick={() => onChange(!on)}>
      <span />
    </button>
  );
}

// SaveSwitch is a switch that saves itself, and says so when the server refuses.
export function SaveSwitch({ label, on, onSave }: { label: string; on: boolean; onSave: (on: boolean) => Promise<unknown> }) {
  const [busy, setBusy] = useState(false);
  const notify = useNotify();
  return (
    <Switch label={label} on={on} disabled={busy} onChange={async (v) => {
      setBusy(true);
      try {
        await onSave(v);
      } catch (e) {
        notify(message(e), "bad");
      } finally {
        setBusy(false);
      }
    }} />
  );
}

// SaveField is a text field that saves itself when it loses focus or Enter is
// pressed, if it changed, and says so beside it.
export function SaveField({ label, value, onSave, placeholder, mono, numeric, width, type }: {
  label: string;
  value: string;
  onSave: (value: string) => Promise<unknown>;
  placeholder?: string;
  mono?: boolean;
  numeric?: boolean;
  width?: number;
  type?: string;
}) {
  const [text, setText] = useState(value);
  const [state, setState] = useState<"" | "saving" | "saved">("");
  const notify = useNotify();
  useEffect(() => setText(value), [value]);
  useEffect(() => {
    if (state !== "saved") return;
    const t = setTimeout(() => setState(""), 1600);
    return () => clearTimeout(t);
  }, [state]);

  const save = async () => {
    if (text === value) return;
    setState("saving");
    try {
      await onSave(text);
      setState("saved");
    } catch (e) {
      setState("");
      setText(value);
      notify(message(e), "bad");
    }
  };

  return (
    <span className="save-field">
      <SaveMark state={state} />
      <input className={"input" + (mono ? " mono" : "")} aria-label={label} value={text} placeholder={placeholder} type={type ?? "text"}
        inputMode={numeric ? "numeric" : undefined} style={width ? { width } : undefined}
        autoComplete="off" spellCheck={false} data-1p-ignore data-lpignore="true"
        onChange={(e) => setText(numeric ? e.target.value.replace(/\D/g, "").slice(0, 5) : e.target.value)}
        onBlur={save}
        onKeyDown={(e) => e.key === "Enter" && (e.target as HTMLInputElement).blur()} />
    </span>
  );
}

export function SaveMark({ state }: { state: "" | "saving" | "saved" }) {
  if (state === "saving") return <span className="spinner small" aria-label="Saving" />;
  if (state === "saved") return <span className="saved"><Icon name="check" size={16} />Saved</span>;
  return null;
}

// ArmedButton asks twice: the first press arms it for a few seconds.
export function ArmedButton({ icon, label, armedLabel, onConfirm, small }: { icon: IconName; label: string; armedLabel: string; onConfirm: () => void; small?: boolean }) {
  const [armed, setArmed] = useState(false);
  useEffect(() => {
    if (!armed) return;
    const t = setTimeout(() => setArmed(false), 4000);
    return () => clearTimeout(t);
  }, [armed]);
  return (
    <button type="button" className={"btn danger" + (small ? " small" : "") + (armed ? " armed" : "")} onClick={() => (armed ? onConfirm() : setArmed(true))}>
      <Icon name={icon} size={18} />
      <span>{armed ? armedLabel : label}</span>
    </button>
  );
}

export function Callout({ tone, icon, children, action }: { tone?: "bad" | "warn" | "ok"; icon: IconName; children: ReactNode; action?: ReactNode }) {
  return (
    <div className={"callout" + (tone ? " " + tone : "") + (action ? " held" : "")}>
      <Icon name={icon} />
      <span>{children}</span>
      {action}
    </div>
  );
}

// Notices pop up at the bottom left for a few seconds.
type Notify = (text: string, tone?: "ok" | "bad") => void;
const NotifyContext = createContext<Notify>(() => {});
export const useNotify = () => useContext(NotifyContext);

export function Notices({ children }: { children: ReactNode }) {
  const [toast, setToast] = useState<{ text: string; tone: "ok" | "bad"; at: number } | null>(null);
  useEffect(() => {
    if (!toast) return;
    const t = setTimeout(() => setToast(null), 5000);
    return () => clearTimeout(t);
  }, [toast]);
  const notify = useCallback<Notify>((text, tone = "ok") => setToast({ text, tone, at: Date.now() }), []);
  return (
    <NotifyContext.Provider value={notify}>
      {children}
      {toast && (
        <div key={toast.at} className={"toast " + toast.tone} role={toast.tone === "bad" ? "alert" : "status"}>
          <Icon name={toast.tone === "bad" ? "error" : "check_circle"} />
          <span>{toast.text}</span>
        </div>
      )}
    </NotifyContext.Provider>
  );
}

// Copy is a value that copies itself when clicked.
export function Copy({ text, copyText, plain }: { text: string; copyText?: string; plain?: boolean }) {
  const [done, setDone] = useState(false);
  return (
    <button type="button" className={"copy" + (done ? " done" : "")} title="Copy"
      onClick={() => {
        navigator.clipboard?.writeText(copyText ?? text);
        setDone(true);
        setTimeout(() => setDone(false), 2500);
      }}>
      <span className={"copy-text" + (plain ? "" : " mono")}>{text}</span>
      <Icon name={done ? "check" : "content_copy"} size={16} className="copy-icon" />
    </button>
  );
}

// TextField is an outlined field whose label rests inside it and floats onto
// the outline once it has focus or a value. `trailing` sits at its right end,
// such as a Show button or a spinner; `hint` is the line under it.
export function TextField({ label, value, onChange, hint, trailing, mono, className, ...rest }: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  hint?: ReactNode;
  trailing?: ReactNode;
  mono?: boolean;
} & Omit<InputHTMLAttributes<HTMLInputElement>, "value" | "onChange">) {
  const id = useId();
  const input = "tf-input" + (mono ? " mono" : "");
  return (
    <div className={"tf" + (trailing ? " has-trailing" : "") + (className ? " " + className : "")}>
      <div className="tf-box">
        <input id={id} className={input} value={value} spellCheck={false} {...rest} placeholder={rest.placeholder ?? " "}
          onChange={(e) => onChange(e.target.value)} />
        <label className="tf-label" htmlFor={id}>{label}</label>
        {trailing && <span className="tf-trailing">{trailing}</span>}
      </div>
      {hint && <div className="tf-hint">{hint}</div>}
    </div>
  );
}

// PasswordField is a password with Show, and with generate, a button that makes
// a strong one.
export function PasswordField({ label, value, onChange, hint, generate, autoComplete, autoFocus, className }: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  hint?: ReactNode;
  generate?: boolean;
  autoComplete?: string;
  autoFocus?: boolean;
  className?: string;
}) {
  const [shown, setShown] = useState(false);
  return (
    <TextField className={(className ?? "") + (generate ? " two-actions" : "")} label={label} type={shown ? "text" : "password"}
      autoComplete={autoComplete ?? "new-password"} autoFocus={autoFocus} mono={shown && !!value} value={value} onChange={onChange} hint={hint}
      trailing={
        <>
          <button type="button" className="tf-action" onClick={() => setShown((s) => !s)}>{shown ? "Hide" : "Show"}</button>
          {generate && (
            <button type="button" className="tf-action" onClick={() => {
              onChange(generatePassword());
              setShown(true);
            }}>Generate</button>
          )}
        </>
      } />
  );
}

// SubNav is the bar under a page's heading, as in EPS: the way back, a short
// rule, then the page's sub-pages, the open one underlined in the accent colour.
export function SubNav({ back, backLabel, base, items, current }: {
  back: string;
  backLabel: string;
  base: string;
  items: [string, string, IconName][];
  current: string;
}) {
  return (
    <nav className="sub-nav" aria-label="Sub-pages">
      <a className="sub-nav-back" href={back}><Icon name="arrow_back" size={18} />{backLabel}</a>
      <span className="sub-nav-rule" />
      {items.map(([key, label, icon]) => (
        <a key={key} className={"sub-nav-item" + (current === key ? " on" : "")} href={`${base}/${key}`} aria-current={current === key ? "page" : undefined}>
          <Icon name={icon} size={18} />
          <span>{label}</span>
        </a>
      ))}
    </nav>
  );
}

// Avatar is a round mark with someone's initials, in a colour of their own.
export function Avatar({ name, big }: { name: string; big?: boolean }) {
  const initials = name.split(/[\s._@-]+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join("") || "?";
  let hue = 0;
  for (const c of name) hue = (hue * 31 + c.charCodeAt(0)) % 360;
  return <span className={"avatar" + (big ? " big" : "")} style={{ backgroundColor: `hsl(${hue} 45% 42%)` }} aria-hidden>{initials}</span>;
}

// Filters is a row of chips that picks one view of a list, each with its count.
export function Filters<T extends string>({ value, onChange, items }: { value: T; onChange: (v: T) => void; items: [T, string, number?][] }) {
  return (
    <div className="filters" role="tablist">
      {items.map(([key, label, count]) => (
        <button key={key} type="button" role="tab" className="filter" aria-selected={value === key} onClick={() => onChange(key)}>
          {label}{count !== undefined && <span>{count.toLocaleString()}</span>}
        </button>
      ))}
    </div>
  );
}

// Tally is a row of figures, each tinted by how it reads.
export function Tally({ items }: { items: { label: string; value: ReactNode; tone?: "ok" | "warn" | "bad" | "plain" }[] }) {
  const size = items.length === 3 ? " three" : items.length === 4 ? " four" : "";
  return (
    <div className={"tally" + size}>
      {items.map((t) => <div key={t.label} className={"tally-item" + (t.tone ? " " + t.tone : "")}><b>{t.value}</b><span>{t.label}</span></div>)}
    </div>
  );
}

// Welcome fills an empty list: what goes there, and how to add the first one.
export function Welcome({ icon, title, children, action }: { icon: IconName; title: string; children?: ReactNode; action?: ReactNode }) {
  return (
    <div className="card welcome">
      <Icon name={icon} size={40} />
      <h2>{title}</h2>
      {children && <p className="muted">{children}</p>}
      {action}
    </div>
  );
}

// usePopover places a panel over the page under its button, so no card or scroll
// area cuts it off. A click outside, Escape, scrolling the page or resizing closes it.
export function usePopover(width: number) {
  const [open, setOpen] = useState(false);
  const [at, setAt] = useState({ top: 0, left: 0 });
  const anchor = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useDismiss(open, close, anchor, panel);
  useLayoutEffect(() => {
    if (!open || !anchor.current) return;
    const r = anchor.current.getBoundingClientRect();
    setAt({ top: r.bottom + 4, left: Math.max(8, Math.min(r.right - width, innerWidth - width - 8)) });
    const away = (e: Event) => {
      if (!panel.current?.contains(e.target as Node)) close();
    };
    addEventListener("scroll", away, true);
    addEventListener("resize", close);
    return () => {
      removeEventListener("scroll", away, true);
      removeEventListener("resize", close);
    };
  }, [open, close, width]);
  return { open, setOpen, close, anchor, panel, style: { top: at.top, left: at.left, width } };
}

export interface MenuAction {
  label: string;
  icon: IconName;
  onClick: () => void;
  danger?: boolean;
  disabled?: boolean;
  // Asked once more, with this label, before it runs.
  confirm?: string;
}

// MoreMenu is the three dots that hold the less frequent actions; with icon, a
// different button opens it.
export function MoreMenu({ label, actions, icon = "more_vert", small }: { label: string; actions: MenuAction[]; icon?: IconName; small?: boolean }) {
  const pop = usePopover(260);
  const [armed, setArmed] = useState("");
  return (
    <div className="menu-wrap" ref={pop.anchor}>
      <button type="button" className={"icon-btn" + (small ? " small" : "")} aria-label={label} title={label} aria-expanded={pop.open}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setArmed("");
          pop.setOpen((o) => !o);
        }}>
        <Icon name={icon} size={small ? 18 : 20} />
      </button>
      {pop.open && createPortal(
        <div ref={pop.panel} className="menu pop-menu" style={pop.style} role="menu">
          {actions.map((a) => (
            <button key={a.label} type="button" role="menuitem" disabled={a.disabled}
              className={"menu-item" + (a.danger ? " danger" : "") + (armed === a.label ? " armed" : "")}
              onClick={() => {
                if (a.confirm && armed !== a.label) return setArmed(a.label);
                pop.close();
                a.onClick();
              }}>
              <Icon name={a.icon} size={20} />
              {armed === a.label && a.confirm ? a.confirm : a.label}
            </button>
          ))}
        </div>,
        document.body,
      )}
    </div>
  );
}

// ValueRow shows a setting's value as text, with an edit button that turns it
// into a field with Save and Cancel.
export function ValueRow({ title, value, empty = "None", type, hint, mono, onSave }: {
  title: string;
  value: string;
  empty?: string;
  type?: string;
  hint?: ReactNode;
  mono?: boolean;
  onSave: (value: string) => Promise<unknown>;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(value);
  const [busy, setBusy] = useState(false);
  const notify = useNotify();

  if (editing) {
    const save = async (e: FormEvent) => {
      e.preventDefault();
      setBusy(true);
      try {
        await onSave(draft.trim());
        setEditing(false);
      } catch (err) {
        notify(message(err), "bad");
      } finally {
        setBusy(false);
      }
    };
    return (
      <form className="setting nested" onSubmit={save}>
        <TextField className="grow" label={title} type={type} mono={mono} autoFocus autoComplete="off" value={draft} onChange={setDraft} hint={hint} />
        <button type="button" className="btn text" onClick={() => setEditing(false)}>Cancel</button>
        <button className="btn primary" disabled={busy}>Save</button>
      </form>
    );
  }
  return (
    <Row title={title} sub={value ? (mono ? <span className="mono">{value}</span> : value) : empty}>
      <button type="button" className="icon-btn" aria-label={`Change ${title.toLowerCase()}`} title={`Change ${title.toLowerCase()}`}
        onClick={() => {
          setDraft(value);
          setEditing(true);
        }}>
        <Icon name="edit" size={20} />
      </button>
    </Row>
  );
}

// Busy runs an action from a button, with a spinner while it runs and a notice
// when it fails.
export function useAction() {
  const notify = useNotify();
  const [busy, setBusy] = useState("");
  const run = useCallback(async (key: string, fn: () => Promise<unknown>, done?: string) => {
    setBusy(key);
    try {
      await fn();
      if (done) notify(done);
      return true;
    } catch (e) {
      notify(message(e), "bad");
      return false;
    } finally {
      setBusy("");
    }
  }, [notify]);
  return { busy, run };
}
