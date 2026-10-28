import { timeAgo } from "../format";
import { statusOf, useOverview } from "../overview";
import { href } from "../router";
import { Icon } from "../ui/Icon";

// StatusBar is Docker's engine bar: the server's state on the left, the
// numbers that matter, and how many checks need attention on the right.
export function StatusBar() {
  const { ov, error } = useOverview();
  const st = statusOf(ov);
  const all = [st.cf, st.ts, st.out];
  const problems = (ov?.checks?.items ?? []).filter((i) => !i.ok).length;
  const state = error || all.includes("bad") ? "bad" : all.includes("warn") ? "warn" : all.includes("none") ? "none" : "ok";
  const label = error ? "Server unreachable" : { ok: "Mail server running", warn: "Mail waiting to send", bad: "Mail server needs attention", none: "Checking" }[state];

  return (
    <footer className="statusbar">
      <a className={"engine " + state} href={href("dashboard")}><Icon name="mail" size={16} />{label}</a>
      <span className="stats">
        <span>Accounts {ov?.accounts ?? 0}</span>
        <span>Domains {ov?.domains?.length ?? 0}</span>
        <span>Queue {ov?.queue ?? 0}</span>
        <span>Last received {timeAgo(ov?.last_inbound)}</span>
      </span>
      <span className="statusbar-right">
        <a href={href("logs")}><Icon name="terminal" size={16} />Logs</a>
        {problems > 0 && <a className="warn" href={href("dashboard")}><Icon name="warning" size={16} />{problems} {problems === 1 ? "problem" : "problems"}</a>}
      </span>
    </footer>
  );
}
