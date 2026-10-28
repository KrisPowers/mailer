import { forwardRef, useState, type InputHTMLAttributes, type ReactNode } from "react";
import { Icon } from "./Icon";
import { Select } from "./Select";

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
};

export const TextInput = forwardRef<HTMLInputElement, InputProps>(function TextInput({ value, onChange, className, ...rest }, ref) {
  return (
    <input ref={ref} type="text" spellCheck={false} autoComplete="off" {...rest} value={value}
      className={"input" + (className ? " " + className : "")} onChange={(e) => onChange(e.target.value)} />
  );
});

// AddressInput is one field for an address: the name typed in, then the
// domain, picked right there when there's more than one.
export function AddressInput({ id, name, onName, domain, domains, onDomain, placeholder = "name", autoFocus }: {
  id?: string;
  name: string;
  onName: (v: string) => void;
  domain: string;
  domains: string[];
  onDomain: (d: string) => void;
  placeholder?: string;
  autoFocus?: boolean;
}) {
  return (
    <div className="input address">
      <input id={id} type="text" spellCheck={false} autoComplete="off" autoFocus={autoFocus} placeholder={placeholder}
        value={name} onChange={(e) => onName(e.target.value)} />
      <span className="at">@</span>
      {domains.length > 1
        ? <Select inline value={domain} options={domains} onChange={onDomain} label="Domain" />
        : <span className="domain">{domain}</span>}
    </div>
  );
}

// PasswordInput hides its value until the eye is clicked.
export function PasswordInput({ value, onChange, id, placeholder, autoFocus, autoComplete = "new-password" }: {
  value: string;
  onChange: (v: string) => void;
  id?: string;
  placeholder?: string;
  autoFocus?: boolean;
  autoComplete?: string;
}) {
  const [shown, setShown] = useState(false);
  return (
    <div className="input with-button">
      <input id={id} type={shown ? "text" : "password"} autoComplete={autoComplete} spellCheck={false} value={value}
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
