import { useOverview } from "../overview";
import { href, type View } from "../router";
import { Icon, type IconName } from "../ui/Icon";

export function Sidebar({ view }: { view: View }) {
  const { ov } = useOverview();
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
      {item("queue", "outbox", "Queue", ov?.queue)}
      {item("logs", "terminal", "Logs")}
    </nav>
  );
}
