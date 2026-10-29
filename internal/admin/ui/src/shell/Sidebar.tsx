import { useOverview } from "../overview";
import { href, type View } from "../router";
import { useUser } from "../session";
import { Icon, type IconName } from "../ui/Icon";

export function Sidebar({ view }: { view: View }) {
  const { ov } = useOverview();
  const admin = useUser()?.role === "admin";
  const item = (v: View, icon: IconName, label: string, count?: number) => (
    <a href={href(v)} className={"nav-item" + (view === v ? " active" : "")} aria-current={view === v ? "page" : undefined}>
      <Icon name={icon} />
      <span className="nav-label">{label}</span>
      {!!count && <span className="nav-count">{count}</span>}
    </a>
  );
  return (
    <nav className="sidebar" aria-label="Main">
      {item("accounts", "group", "Accounts", ov?.accounts)}
      {item("flagged", "flag", "Flagged", ov?.flagged)}
      {item("queue", "outbox", "Queue", ov?.queue)}
      {item("logs", "terminal", "Logs")}
      <div className="nav-section">Admin</div>
      {admin && item("access", "devices", "Access", ov?.pending_devices)}
      {item("settings", "settings", "Settings")}
      {item("docs", "menu_book", "Docs")}
    </nav>
  );
}
