import { forwardRef, useState, type InputHTMLAttributes, type ReactNode } from "react";
import { Icon } from "./Icon";

// Field labels a control, with an optional hint under it.
export function Field({ id, label, hint, children }: { id: string; label: string; hint?: ReactNode; children: ReactNode }) {
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      {children}
      {hint && <div className="hint">{hint}</div>}
    </div>
  );
}

type InputProps = Omit<InputHTMLAttributes<HTMLInputElement>, "onChange" | "value"> & {
  value: string;
  onChange: (v: string) => void;
  suffix?: string;
};

export const TextInput = forwardRef<HTMLInputElement, InputProps>(function TextInput({ value, onChange, suffix, className, ...rest }, ref) {
  const input = (
    <input ref={ref} type="text" spellCheck={false} autoComplete="off" {...rest} value={value}
      className={suffix ? undefined : "input" + (className ? " " + className : "")} onChange={(e) => onChange(e.target.value)} />
  );
  return suffix ? <div className="input with-suffix">{input}<span>{suffix}</span></div> : input;
});

// PasswordInput hides its value until the eye is clicked.
export function PasswordInput({ value, onChange, id, placeholder, autoFocus }: {
  value: string;
  onChange: (v: string) => void;
  id?: string;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  const [shown, setShown] = useState(false);
  return (
    <div className="input with-button">
      <input id={id} type={shown ? "text" : "password"} autoComplete="new-password" spellCheck={false} value={value}
        placeholder={placeholder} autoFocus={autoFocus} onChange={(e) => onChange(e.target.value)} />
      <button type="button" className="icon-btn" aria-label={shown ? "Hide" : "Show"} data-tip={shown ? "Hide" : "Show"} onClick={() => setShown(!shown)}>
        <Icon name={shown ? "visibility_off" : "visibility"} size={18} />
      </button>
    </div>
  );
}

// SearchField is the outlined search box above each table.
export function SearchField({ value, onChange, placeholder = "Search" }: { value: string; onChange: (v: string) => void; placeholder?: string }) {
  return (
    <div className="search-field">
      <Icon name="search" />
      <input type="text" role="searchbox" aria-label={placeholder} placeholder={placeholder} spellCheck={false} value={value}
        onChange={(e) => onChange(e.target.value)} onKeyDown={(e) => e.key === "Escape" && onChange("")} />
      {value && (
        <button type="button" className="icon-btn" aria-label="Clear" data-tip="Clear" onClick={() => onChange("")}>
          <Icon name="close" size={18} />
        </button>
      )}
    </div>
  );
}

// Stepper picks a whole number between min and max.
export function Stepper({ id, value, min, max, onChange }: { id?: string; value: number; min: number; max: number; onChange: (v: number) => void }) {
  const [text, setText] = useState(String(value));
  const set = (v: number) => {
    const n = Math.min(max, Math.max(min, Math.round(v)));
    setText(String(n));
    if (n !== value) onChange(n);
  };
  return (
    <div className="stepper">
      <button type="button" className="icon-btn" aria-label="Less" data-tip="Less" disabled={value <= min} onClick={() => set(value - 1)}>
        <Icon name="remove" size={18} />
      </button>
      <input id={id} type="text" inputMode="numeric" value={text} onChange={(e) => setText(e.target.value.replace(/\D/g, ""))}
        onBlur={() => set(Number(text) || min)} onKeyDown={(e) => e.key === "Enter" && set(Number(text) || min)} />
      <button type="button" className="icon-btn" aria-label="More" data-tip="More" disabled={value >= max} onClick={() => set(value + 1)}>
        <Icon name="add" size={18} />
      </button>
    </div>
  );
}
