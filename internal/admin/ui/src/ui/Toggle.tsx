import type { ReactNode } from "react";
import { Icon } from "./Icon";

// Toggle is the small switch Docker uses for filters and settings.
export function Toggle({ checked, onChange, disabled, children }: {
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  children?: ReactNode;
}) {
  return (
    <label className={"toggle" + (disabled ? " disabled" : "")}>
      <input type="checkbox" role="switch" className="sr" checked={checked} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
      <span className="track" aria-hidden="true"><span className="thumb" /></span>
      {children && <span className="toggle-label">{children}</span>}
    </label>
  );
}

// Checkbox selects table rows; "mixed" marks a partly selected header.
export function Checkbox({ checked, mixed, onChange, label }: {
  checked: boolean;
  mixed?: boolean;
  onChange: (v: boolean) => void;
  label: string;
}) {
  return (
    <label className="checkbox">
      <input type="checkbox" className="sr" aria-label={label} checked={checked}
        ref={(el) => { if (el) el.indeterminate = !!mixed; }} onChange={(e) => onChange(e.target.checked)} />
      <span className="box" aria-hidden="true">{(checked || mixed) && <Icon name={mixed ? "remove" : "check"} size={16} />}</span>
    </label>
  );
}
