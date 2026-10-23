import { Icon, type IconName } from "../components/Icon";
import { useOverview } from "../overview";
import type { View } from "../router";

interface Props {
  view: View;
  collapsed: boolean;
  onCollapse: () => void;
}

export function Sidebar({ view, collapsed, onCollapse }: Props) {
  const { ov } = useOverview();
  const link = (v: View, icon: IconName, label: string, count?: number, warn?: boolean) => (
    <a href={"#" + v} className={view === v ? "active" : ""}>
      <Icon name={icon} />
      <span>{label}</span>
      {!!count && <em className={"count" + (warn ? " warn" : "")}>{count}</em>}
    </a>
  );
  return (
    <nav className="sidebar">
      {link("dashboard", "grid", "Dashboard")}
      {link("accounts", "users", "Accounts", ov?.accounts)}
      {link("aliases", "at", "Aliases")}
      {link("queue", "send", "Outbound queue", ov?.queue, true)}
      {link("logs", "terminal", "Logs")}
      <div className="nav-spacer" />
      {link("settings", "sliders", "Settings")}
      <button className="collapse" title={collapsed ? "Expand sidebar" : "Collapse sidebar"} onClick={onCollapse}>
        <Icon name="collapse" />
      </button>
    </nav>
  );
}
