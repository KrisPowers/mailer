import { useEffect, type ReactNode } from "react";
import { api } from "../api";
import { Repair } from "../flows/Repair";
import { Setup } from "../flows/Setup";
import { useEnableTailscale } from "../flows/tailscale";
import { setupStep, useOverview } from "../overview";
import { href } from "../router";
import { Button } from "../ui/Button";
import { useModal } from "../ui/Modal";
import { Dot, EmptyRow, Metrics } from "../ui/Page";
import { useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

interface Item {
  key: string;
  bad: boolean;
  text: ReactNode;
  action: ReactNode;
}

// Dashboard is four figures and a list of what needs doing, each with the
// button that does it. Nothing to do means an empty list.
export function Dashboard() {
  const { ov, reload } = useOverview();
  const accounts = useLoad(api.accounts);
  const signIns = useLoad(api.signIns);
  const { open } = useModal();
  const toast = useToast();
  const enableFunnel = useEnableTailscale();

  useEffect(() => {
    accounts.reload();
    signIns.reload();
  }, [ov]);

  const unread = (accounts.data ?? []).reduce((n, a) => n + a.unseen, 0);
  const day = Date.now() - 864e5;
  const failed = (signIns.data?.events ?? []).filter((e) => e.user && e.result !== "ok" && e.result !== "no_sign_in" && new Date(e.at).getTime() > day);

  const repair = () => open(<Repair domains={ov?.domains ?? []} />);
  const items: Item[] = [];
  const step = setupStep(ov);
  if (step) {
    items.push({ key: "setup", bad: true, text: <b>{["", "Connect Cloudflare", "Enable Tailscale Funnel", "Create the first account"][step]}</b>,
      action: <Button variant="primary" small onClick={() => open(<Setup />)}>Continue setup</Button> });
  }
  for (const c of ov?.checks?.items ?? []) {
    if (c.ok || (step === 1 && c.area === "cloudflare") || (step === 2 && c.area === "tailscale")) continue;
    items.push({
      key: c.name, bad: true,
      text: <><b>{c.name}</b><span className="muted"> · {c.detail}</span></>,
      action: c.area === "tailscale"
        ? <Button variant="primary" small onClick={() => enableFunnel()}>Enable Funnel</Button>
        : <Button variant="primary" small onClick={repair}>Repair</Button>,
    });
  }
  for (const b of signIns.data?.blocked ?? []) {
    items.push({ key: "ip" + b.ip, bad: true, text: <><b className="mono">{b.ip}</b><span className="muted"> · blocked from signing in</span></>,
      action: <Button variant="primary" small onClick={async () => {
        await api.unblock(b.ip);
        toast("Unblocked " + b.ip);
        await signIns.reload();
      }}>Unblock</Button> });
  }
  if (ov?.queue) {
    items.push({ key: "queue", bad: false, text: <b>{ov.queue} waiting to send</b>,
      action: <Button variant="primary" small onClick={async () => {
        await api.retryQueue();
        toast("Retrying now");
        setTimeout(reload, 2500);
      }}>Retry now</Button> });
  }
  const byUser = new Map<string, number>();
  failed.forEach((e) => byUser.set(e.user!, (byUser.get(e.user!) ?? 0) + 1));
  for (const [user, n] of byUser) {
    items.push({ key: "fail" + user, bad: false, text: <><b>{user}</b><span className="muted"> · {n} failed sign-in{n === 1 ? "" : "s"} today</span></>,
      action: <a className="btn primary small" href={href("accounts", user)}>Open</a> });
  }

  return (
    <>
      <Metrics items={[
        { label: "Accounts", value: ov?.accounts ?? 0 },
        { label: "Unread", value: unread.toLocaleString() },
        { label: "Waiting to send", value: ov?.queue ?? 0 },
        { label: "Failed sign-ins today", value: failed.length },
      ]} />
      <div className="table-wrap">
        <table className="table">
          <thead><tr><th className="col-dot" /><th className="grow">Needs attention</th><th className="col-actions">Action</th></tr></thead>
          <tbody>
            {!ov?.checks ? <EmptyRow cols={3}>Checking…</EmptyRow>
              : !items.length ? <EmptyRow cols={3} icon="check_circle">Nothing needs attention</EmptyRow>
              : items.map((i) => (
                <tr key={i.key}>
                  <td className="col-dot"><Dot health={i.bad ? "bad" : "warn"} /></td>
                  <td className="wrap">{i.text}</td>
                  <td className="col-actions">{i.action}</td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
