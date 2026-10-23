import type { ReactNode } from "react";

interface Props {
  checked: boolean;
  onChange: (v: boolean) => void;
  title: string;
  children?: ReactNode;
}

// Switch is a labelled on/off row used on settings-style pages.
export function Switch({ checked, onChange, title, children }: Props) {
  return (
    <div className="switch-row">
      <label className="switch">
        <input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />
        <span />
      </label>
      <div className="txt">
        <b>{title}</b>
        <span className="muted">{children}</span>
      </div>
    </div>
  );
}
