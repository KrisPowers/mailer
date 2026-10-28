import { Repair } from "../flows/Repair";
import { useEnableTailscale } from "../flows/tailscale";
import { statusOf, useOverview } from "../overview";
import { href } from "../router";
import { Button } from "../ui/Button";
import { PopoverButton, useFloating } from "../ui/Floating";
import { Icon } from "../ui/Icon";
import { useModal } from "../ui/Modal";

// StatusBar is Docker's engine bar: the server's state on the left, the
// numbers that matter, and any failing checks on the right, each with
// the button that fixes it.
export function StatusBar() {
  const { ov, error } = useOverview();
  const st = statusOf(ov);
  const all = [st.cf, st.ts, st.out];
  const failing = (ov?.checks?.items ?? []).filter((i) => !i.ok);
  const state = error || all.includes("bad") ? "bad" : all.includes("warn") ? "warn" : all.includes("none") ? "none" : "ok";
  const label = error ? "Server unreachable" : { ok: "Mail server running", warn: "Mail waiting to send", bad: "Mail server needs attention", none: "Checking" }[state];

  return (
    <footer className="statusbar">
      <span className={"engine " + state}><Icon name="mail" size={16} />{label}</span>
      <span className="stats">
        <span>Accounts {ov?.accounts ?? 0}</span>
        <span>Domains {ov?.domains?.length ?? 0}</span>
        <span>Queue {ov?.queue ?? 0}</span>
      </span>
      <span className="statusbar-right">
        <a href={href("logs")}><Icon name="terminal" size={16} />Logs</a>
        {failing.length > 0 && (
          <PopoverButton className="problems" label="What needs fixing" content={<Problems />}>
            <Icon name="warning" size={16} />{failing.length} {failing.length === 1 ? "problem" : "problems"}
          </PopoverButton>
        )}
      </span>
    </footer>
  );
}

// Problems lists each failing check with the button that fixes it.
function Problems() {
  const { ov } = useOverview();
  const { open } = useModal();
  const { close } = useFloating();
  const enableFunnel = useEnableTailscale();
  const failing = (ov?.checks?.items ?? []).filter((i) => !i.ok);
  return (
    <ul className="problem-list">
      {failing.map((c) => (
        <li key={c.name}>
          <div><b>{c.name}</b><div className="muted small">{c.detail}</div></div>
          {c.area === "tailscale"
            ? <Button variant="primary" small onClick={() => { close(); return enableFunnel(); }}>Enable Funnel</Button>
            : <Button variant="primary" small onClick={() => { close(); open(<Repair domains={ov?.domains ?? []} />); }}>Repair</Button>}
        </li>
      ))}
    </ul>
  );
}
