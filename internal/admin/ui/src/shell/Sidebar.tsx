import { Icon, type IconName } from "../components/Icon";
import { useOverview } from "../overview";
import type { View } from "../router";

export function Sidebar({ view, onCollapse }: { view: View; onCollapse: () => void }) {
  const { ov } = useOverview();
  const link = (v: View, icon: IconName, label: string, count?: number, warn?: boolean) => (
    <a href={"#" + v} className={view === v ? "active" : ""} title={label}>
      <Icon name={icon} />
      <span>{label}</span>
      {!!count && <em className={"count" + (warn ? " warn" : "")}>{count}</em>}
    </a>
  );
  return (
    <nav className="sidebar">
      <button className="icon-btn side-toggle" title="Collapse menu" onClick={onCollapse}><Icon name="sidebar" /></button>
      {link("dashboard", "grid", "Dashboard")}
      {link("accounts", "users", "Accounts", ov?.accounts)}
      {link("domains", "globe", "Domains", ov?.domains?.length)}
      {link("aliases", "at", "Aliases")}
      {link("queue", "send", "Outbound queue", ov?.queue, true)}
      {link("signins", "key", "Sign-ins")}
      {link("logs", "terminal", "Logs")}
      <hr />
      {link("settings", "settings", "Settings")}
    </nav>
  );
}
