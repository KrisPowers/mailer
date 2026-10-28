import type { ReactNode } from "react";
import { Icon, type IconName } from "./Icon";

export function PageHead({ title, sub, children }: { title: string; sub?: ReactNode; children?: ReactNode }) {
  return (
    <div className="page-head">
      <h1>{title}</h1>
      {sub && <span className="sub">{sub}</span>}
      <div className="actions">{children}</div>
    </div>
  );
}

interface PanelProps {
  title?: string;
  icon?: IconName;
  actions?: ReactNode;
  children: ReactNode;
}

export function Panel({ title, icon, actions, children }: PanelProps) {
  return (
    <div className="panel">
      {title && (
        <div className="panel-head">
          {icon && <Icon name={icon} />}
          {title}
          <div className="actions">{actions}</div>
        </div>
      )}
      {children}
    </div>
  );
}

// Kpi is one figure in the summary row, label above value.
export function Kpi({ value, label }: { value: ReactNode; label: string }) {
  return (
    <div className="kpi">
      <div className="lbl">{label}</div>
      <div className="val">{value}</div>
    </div>
  );
}

export function EmptyRow({ cols, children }: { cols: number; children: ReactNode }) {
  return <tr><td colSpan={cols} className="empty">{children}</td></tr>;
}

export function Search({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder: string }) {
  return (
    <div className="search">
      <Icon name="search" />
      <input type="search" placeholder={placeholder} value={value} onChange={(e) => onChange(e.target.value)} />
    </div>
  );
}
