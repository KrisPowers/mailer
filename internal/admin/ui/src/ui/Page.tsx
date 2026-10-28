import type { ReactNode } from "react";
import type { Health } from "../overview";
import { IconButton } from "./Button";
import { Icon, type IconName } from "./Icon";
import { useCopy } from "./Toast";

// PageHeader is the title row: the page name, a small link beside it, and
// the page's main actions on the right. With back, it's a record page.
export function PageHeader({ title, link, back, actions }: { title: ReactNode; link?: ReactNode; back?: string; actions?: ReactNode }) {
  return (
    <div className="page-header">
      {back && <a className="icon-btn" href={back} aria-label="Back" data-tip="Back"><Icon name="arrow_back" /></a>}
      <h1>{title}</h1>
      {link}
      {actions && <div className="page-actions">{actions}</div>}
    </div>
  );
}

export interface Metric {
  label: string;
  value: ReactNode;
  note?: ReactNode;
  tip?: string;
}

// Metrics is the row of figures under the title, like Docker's CPU and
// memory usage.
export function Metrics({ items, aside }: { items: Metric[]; aside?: ReactNode }) {
  return (
    <div className="metrics">
      {items.map((m) => (
        <div className="metric" key={m.label}>
          <div className="metric-label">
            {m.label}
            {m.tip && <span className="info" tabIndex={0} data-tip={m.tip} aria-label={m.tip}><Icon name="info" size={16} /></span>}
          </div>
          <div className="metric-value">{m.value}{m.note && <small>{m.note}</small>}</div>
        </div>
      ))}
      {aside && <div className="metrics-aside">{aside}</div>}
    </div>
  );
}

// Toolbar sits between the metrics and a table: search and filters on the
// left, actions on the right.
export function Toolbar({ children, actions }: { children?: ReactNode; actions?: ReactNode }) {
  return (
    <div className="toolbar">
      {children}
      {actions && <div className="toolbar-actions">{actions}</div>}
    </div>
  );
}

export function Showing({ count, noun = "item" }: { count: number; noun?: string }) {
  return <div className="showing">Showing {count} {count === 1 ? noun : noun + "s"}</div>;
}

export function Section({ title, actions, children }: { title: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <section className="section">
      <div className="section-head"><h2>{title}</h2>{actions && <div className="section-actions">{actions}</div>}</div>
      {children}
    </section>
  );
}

const healthLabel: Record<Health, string> = { ok: "OK", warn: "Waiting", bad: "Problem", none: "Checking" };

// Dot is a table row's status light; it always has a text label for
// screen readers and a tooltip.
export function Dot({ health, label }: { health: Health; label?: string }) {
  const text = label ?? healthLabel[health];
  return <span className={"dot " + health} role="img" aria-label={text} data-tip={text} />;
}

// Chip is a short status or tag, coloured by health.
export function Chip({ health = "none", children }: { health?: Health | "plain"; children: ReactNode }) {
  return <span className={"chip " + health}>{children}</span>;
}

export function Tabs<T extends string>({ tabs, value, onChange }: { tabs: [T, string][]; value: T; onChange: (t: T) => void }) {
  return (
    <div className="tabs" role="tablist">
      {tabs.map(([key, label]) => (
        <button key={key} type="button" role="tab" aria-selected={key === value} className={"tab" + (key === value ? " active" : "")}
          onClick={() => onChange(key)}>{label}</button>
      ))}
    </div>
  );
}

export interface KV {
  label: string;
  value: string;
  mono?: boolean;
  copy?: boolean;
}

// KeyValues lists settings, each copyable when it's a real value.
export function KeyValues({ rows }: { rows: KV[] }) {
  const copy = useCopy();
  return (
    <dl className="kv">
      {rows.map((r) => [
        <dt key={r.label + "-k"}>{r.label}</dt>,
        <dd key={r.label + "-v"} className={r.mono ? "mono" : r.copy === false ? "muted" : ""}>{r.value}</dd>,
        r.copy === false ? <span key={r.label + "-c"} />
          : <IconButton key={r.label + "-c"} icon="content_copy" label={"Copy " + r.label.toLowerCase()} onClick={() => copy(r.value, r.label + " copied")} />,
      ])}
    </dl>
  );
}

export function EmptyRow({ cols, icon, children }: { cols: number; icon?: IconName; children: ReactNode }) {
  return (
    <tr className="empty-row">
      <td colSpan={cols}>
        <div className="empty">{icon && <Icon name={icon} size={32} />}<div>{children}</div></div>
      </td>
    </tr>
  );
}

// Steps lists the outcome of each setup step.
export function Steps({ steps, error }: { steps?: { name: string; ok: boolean; detail?: string }[]; error?: string }) {
  return (
    <ul className="steps">
      {(steps ?? []).map((s, i) => (
        <li key={i} className={s.ok ? "ok" : "bad"}>
          <Icon name={s.ok ? "check_circle" : "error"} size={18} />
          <span>{s.name}</span>
          {s.detail && <span className="muted">{s.detail}</span>}
        </li>
      ))}
      {error && !steps?.some((s) => !s.ok) && <li className="bad"><Icon name="error" size={18} /><span>{error}</span></li>}
    </ul>
  );
}
