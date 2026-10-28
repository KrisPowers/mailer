import { useEffect } from "react";
import { api } from "../api";
import { fmtSize, fmtTime } from "../format";
import { useOverview, type Health } from "../overview";
import { IconButton } from "../ui/Button";
import { PopoverButton } from "../ui/Floating";
import { Icon } from "../ui/Icon";
import { Dot, EmptyRow } from "../ui/Page";
import { useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

const STATUS: Record<string, [Health, string]> = {
  waiting: ["warn", "Waiting to send"],
  delivered: ["ok", "Delivered"],
  junk: ["warn", "Delivered to Junk"],
  rejected: ["bad", "Rejected"],
  failed: ["bad", "Failed, sender retries"],
  sent: ["ok", "Sent"],
  bounced: ["bad", "Bounced"],
};

interface Row {
  key: string;
  at: string;
  dir: "in" | "out";
  from: string;
  to: string;
  size?: number;
  status: string;
  detail?: string;
}

// Queue is all mail, received and sent: what's still waiting first, then
// what happened to everything else, newest first.
export function Queue() {
  const queue = useLoad(api.queue);
  const mail = useLoad(api.mail);
  const { reload: reloadOverview } = useOverview();
  const toast = useToast();

  useEffect(() => {
    const t = setInterval(() => { queue.reload(); mail.reload(); }, 5000);
    return () => clearInterval(t);
  }, []);

  const waiting: Row[] = (queue.data ?? []).flatMap((it) => it.rcpts.filter((r) => r.status === "pending").map((r) => ({
    key: it.id + r.addr, at: it.created, dir: "out" as const, from: it.from, to: r.addr, status: "waiting",
    detail: r.last_error ? `Attempt ${it.attempts}, next try ${fmtTime(it.next_try)}: ${r.last_error}` : undefined,
  })));
  const done: Row[] = (mail.data ?? []).map((e, i) => ({ key: "m" + i, ...e }));
  const rows = [...waiting, ...done];
  const error = queue.error || mail.error;

  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            <th className="col-dot" /><th>Time</th><th className="col-dir" /><th>From</th><th>To</th><th>Status</th><th className="num grow">Size</th>
            <th className="col-actions">Actions</th>
          </tr>
        </thead>
        <tbody>
          {error ? <EmptyRow cols={8} icon="error">{error}</EmptyRow>
            : !queue.data || !mail.data ? <EmptyRow cols={8}>Loading…</EmptyRow>
            : !rows.length ? <EmptyRow cols={8} icon="outbox">No mail yet</EmptyRow>
            : rows.map((r) => {
              const [health, label] = STATUS[r.status] ?? ["none", r.status];
              return (
                <tr key={r.key}>
                  <td className="col-dot"><Dot health={health} label={label} /></td>
                  <td>{fmtTime(r.at)}</td>
                  <td className="col-dir">
                    <span className="dir" data-tip={r.dir === "in" ? "Received" : "Sent"} aria-label={r.dir === "in" ? "Received" : "Sent"}>
                      <Icon name={r.dir === "in" ? "inbox" : "send"} size={18} />
                    </span>
                  </td>
                  <td>{r.from || <span className="muted">(none)</span>}</td>
                  <td>{r.to}</td>
                  <td>{label}</td>
                  <td className="num">{r.size ? fmtSize(r.size) : ""}</td>
                  <td className="col-actions">
                    <span className="row-actions">
                      {r.status === "waiting" && (
                        <IconButton icon="restart_alt" label="Retry now" tone="accent" onClick={async () => {
                          await api.retryQueue();
                          toast("Retrying now");
                          setTimeout(() => { queue.reload(); mail.reload(); reloadOverview(); }, 2500);
                        }} />
                      )}
                      {r.detail && (
                        <PopoverButton label="Details" content={<div className="popover-body"><h3>{label}</h3><p>{r.detail}</p></div>}>
                          <Icon name="info" />
                        </PopoverButton>
                      )}
                    </span>
                  </td>
                </tr>
              );
            })}
        </tbody>
      </table>
    </div>
  );
}
