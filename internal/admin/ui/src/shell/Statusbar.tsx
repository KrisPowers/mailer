import { PopoverButton } from "../components/Floating";
import { Icon } from "../components/Icon";
import { timeAgo } from "../format";
import { statusOf, useOverview } from "../overview";
import { CloudflarePopover, MailFlowPopover, TailscalePopover } from "./StatusPopovers";

// Statusbar is the bottom bar, after Docker Desktop's engine bar: overall
// state on the left, counts, and the three connections on the right.
export function Statusbar() {
  const { ov, error } = useOverview();
  const st = statusOf(ov);
  const all = [st.cf, st.ts, st.out];
  const state = error || all.includes("bad") ? "bad" : all.includes("warn") ? "warn" : all.includes("none") ? "" : "ok";
  const label = error ? "Server unreachable" : state === "bad" ? "Mail server needs attention"
    : state === "warn" ? "Mail waiting to send" : state === "ok" ? "Mail server running" : "Checking…";
  const area = (k: string) => "dot" + (k === "none" ? "" : " " + k);

  return (
    <footer className="statusbar">
      <span className={"sb-engine " + state}><Icon name="mail" />{label}</span>
      <span className="sb-stats">
        <span>Accounts {ov?.accounts ?? 0}</span>
        <span>Domains {ov?.domains?.length ?? 0}</span>
        <span>Queue {ov?.queue ?? 0}</span>
        <span>Last received {timeAgo(ov?.last_inbound)}</span>
      </span>
      <span className="sb-right">
        <PopoverButton className="status-btn" title="Cloudflare: receiving mail" content={<CloudflarePopover />}>
          <span className={area(st.cf)} />Cloudflare
        </PopoverButton>
        <PopoverButton className="status-btn" title="Tailscale: mail apps" content={<TailscalePopover />}>
          <span className={area(st.ts)} />Tailscale
        </PopoverButton>
        <PopoverButton className="status-btn" title="Sending mail" content={<MailFlowPopover />}>
          <span className={area(st.out)} />Sending
        </PopoverButton>
        <a href="#logs"><Icon name="terminal" />Logs</a>
      </span>
    </footer>
  );
}
