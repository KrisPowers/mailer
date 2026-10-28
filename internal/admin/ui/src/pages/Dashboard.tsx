import { useEffect, useState } from "react";
import { api } from "../api";
import { Repair } from "../flows/Repair";
import { Setup } from "../flows/Setup";
import { useEnableTailscale } from "../flows/tailscale";
import { fmtSize, timeAgo } from "../format";
import { setupStep, useOverview } from "../overview";
import { href } from "../router";
import type { Check } from "../types";
import { Button, IconButton, LinkButton } from "../ui/Button";
import { Icon } from "../ui/Icon";
import { useModal } from "../ui/Modal";
import { Dot, EmptyRow, Metrics, PageHeader, Showing, Toolbar } from "../ui/Page";
import { SearchField } from "../ui/Field";
import { Toggle } from "../ui/Toggle";
import { useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

const AREA = { cloudflare: "Cloudflare", tailscale: "Tailscale", sending: "Sending" };

// Dashboard is the server at a glance: the figures that matter and every
// connection check, with the fix beside anything failing.
export function Dashboard() {
  const { ov, reload } = useOverview();
  const accounts = useLoad(api.accounts);
  const signIns = useLoad(api.signIns);
  const { open } = useModal();
  const toast = useToast();
  const enableFunnel = useEnableTailscale();
  const [q, setQ] = useState("");
  const [problemsOnly, setProblemsOnly] = useState(false);

  // Follow the overview's refresh so the figures stay current.
  useEffect(() => {
    accounts.reload();
    signIns.reload();
  }, [ov]);

  const list = accounts.data ?? [];
  const size = list.reduce((n, a) => n + a.size, 0);
  const messages = list.reduce((n, a) => n + a.messages, 0);
  const unread = list.reduce((n, a) => n + a.unseen, 0);
  const day = Date.now() - 864e5;
  const recent = (signIns.data?.events ?? []).filter((e) => e.result !== "no_sign_in" && new Date(e.at).getTime() > day);
  const failed = recent.filter((e) => e.result !== "ok").length;

  const items = ov?.checks?.items ?? [];
  const query = q.toLowerCase();
  const shown = items.filter((i) => (!problemsOnly || !i.ok) && (i.name + " " + i.detail).toLowerCase().includes(query));

  const action = (c: Check) => {
    if (c.area === "cloudflare") {
      return ov?.cloudflare_setup
        ? <IconButton icon="build" label="Repair Cloudflare setup" tone={c.ok ? undefined : "accent"} onClick={() => open(<Repair domains={ov?.domains ?? []} />)} />
        : <IconButton icon="login" label="Connect Cloudflare" tone="accent" onClick={() => open(<Setup step={1} />)} />;
    }
    if (c.area === "tailscale") return <IconButton icon="lan" label="Enable Funnel" tone={c.ok ? undefined : "accent"} onClick={() => enableFunnel()} />;
    return <a className="icon-btn" href={href("queue")} aria-label="Open the outbound queue" data-tip="Open the outbound queue"><Icon name="outbox" /></a>;
  };

  return (
    <>
      <PageHeader title="Dashboard" link={<LinkButton icon="refresh" onClick={async () => {
        await api.check();
        await reload();
        toast("Connections re-checked");
      }}>Re-check now</LinkButton>} />
      {!!setupStep(ov) && (
        <div className="callout info banner">
          <span>Setup isn't finished: {["", "connect Cloudflare", "enable Tailscale Funnel", "create the first account"][setupStep(ov)]}.</span>
          <Button variant="primary" small onClick={() => open(<Setup />)}>Continue setup</Button>
        </div>
      )}
      <Metrics items={[
        { label: "Accounts", value: list.length, note: ` ${messages.toLocaleString()} messages` },
        { label: "Storage used", value: fmtSize(size) },
        { label: "Unread", value: unread.toLocaleString(), tip: "Unread messages in every inbox" },
        { label: "Waiting to send", value: ov?.queue ?? 0, tip: "Messages Cloudflare hasn't taken yet" },
        { label: "Sign-ins, last 24 hours", value: <>{recent.length - failed}<span className="value-sep"> / </span><span className={failed ? "bad-text" : ""}>{failed} failed</span></>, tip: "Mail app sign-ins: succeeded / failed" },
        { label: "Last received", value: timeAgo(ov?.last_inbound) },
      ]} />
      <Toolbar>
        <SearchField value={q} onChange={setQ} />
        <Toggle checked={problemsOnly} onChange={setProblemsOnly}>Only show problems</Toggle>
      </Toolbar>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr><th className="col-dot" /><th>Check</th><th>Area</th><th className="grow">Result</th><th className="col-actions">Actions</th></tr>
          </thead>
          <tbody>
            {!ov?.checks ? <EmptyRow cols={5}>Checking connections…</EmptyRow>
              : !shown.length ? <EmptyRow cols={5} icon="check_circle">{problemsOnly ? "Nothing needs attention." : "No checks match."}</EmptyRow>
              : shown.map((c) => (
                <tr key={c.name}>
                  <td className="col-dot"><Dot health={c.ok ? "ok" : "bad"} /></td>
                  <td className="strong">{c.name}</td>
                  <td className="muted">{AREA[c.area]}</td>
                  <td className="wrap">
                    {c.detail}
                    {!c.ok && c.fix && <div className="fix">{c.fix}</div>}
                  </td>
                  <td className="col-actions">{action(c)}</td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>
      {ov?.checks && <Showing count={shown.length} noun="check" />}
    </>
  );
}
