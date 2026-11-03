import { useCallback, useEffect, useState } from "react";
import { fmtSize, fmtTime, message } from "../../shared/format";
import { Icon } from "../../shared/Icon";
import { Filters, PageHead, Tally, useNotify } from "../../shared/ui";
import { api } from "./api";
import { useOverview, type Health } from "./overview";
import type { MailEntry, QueueItem } from "./types";

const STATUS: Record<string, [Health, string]> = {
  waiting: ["warn", "Waiting to send"],
  delivered: ["ok", "Delivered"],
  junk: ["warn", "Delivered to Junk"],
  rejected: ["bad", "Rejected"],
  failed: ["bad", "Failed, the sender retries"],
  sent: ["ok", "Sent"],
  bounced: ["bad", "Bounced"],
};

interface Line {
  key: string;
  at: string;
  dir: "in" | "out";
  from: string;
  to: string;
  size?: number;
  status: string;
  detail?: string;
}

type Show = "all" | "waiting" | "in" | "out" | "problems";

const problem = (l: Line) => STATUS[l.status]?.[0] === "bad";

// Traffic is all mail, received and sent: what's still waiting first, then
// what happened to everything else, newest first.
export function Traffic() {
  const [queue, setQueue] = useState<QueueItem[] | null>(null);
  const [mail, setMail] = useState<MailEntry[] | null>(null);
  const [show, setShow] = useState<Show>("all");
  const [busy, setBusy] = useState(false);
  const { reload: reloadOverview } = useOverview();
  const notify = useNotify();

  const load = useCallback(async () => {
    const [q, m] = await Promise.allSettled([api.queue(), api.mail()]);
    if (q.status === "fulfilled") setQueue(q.value);
    if (m.status === "fulfilled") setMail(m.value);
  }, []);
  useEffect(() => {
    load();
    const t = setInterval(load, 5000);
    return () => clearInterval(t);
  }, [load]);

  const waiting: Line[] = (queue ?? []).flatMap((it) => it.rcpts.filter((r) => r.status === "pending").map((r) => ({
    key: it.id + r.addr, at: it.created, dir: "out" as const, from: it.from, to: r.addr, status: "waiting",
    detail: r.last_error ? `Attempt ${it.attempts}, next try ${fmtTime(it.next_try)}: ${r.last_error}` : `Next try ${fmtTime(it.next_try)}`,
  })));
  const done: Line[] = (mail ?? []).map((e, i) => ({ key: "m" + i, ...e }));
  const lines = [...waiting, ...done];
  const shown = lines.filter((l) => show === "all" || (show === "waiting" ? l.status === "waiting" : show === "problems" ? problem(l) : l.dir === show && l.status !== "waiting"));

  const retry = async () => {
    setBusy(true);
    try {
      await api.retryQueue();
      notify("Retrying now");
      setTimeout(() => { load(); reloadOverview(); }, 2500);
    } catch (e) {
      notify(message(e), "bad");
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <PageHead title="Traffic" sub="Every message received and sent, and what happened to it"
        actions={waiting.length > 0 && <button type="button" className="btn primary" disabled={busy} onClick={retry}><Icon name="restart_alt" size={18} />Retry now</button>} />
      <div className="page-body stack">
        <Tally items={[
          { label: "Waiting to send", value: waiting.length, tone: waiting.length ? "warn" : "ok" },
          { label: "Received", value: done.filter((l) => l.dir === "in").length, tone: "plain" },
          { label: "Sent", value: done.filter((l) => l.dir === "out").length, tone: "plain" },
          { label: "Problems", value: lines.filter(problem).length, tone: lines.some(problem) ? "bad" : "ok" },
        ]} />
        <Filters<Show> value={show} onChange={setShow} items={[
          ["all", "All", lines.length],
          ["waiting", "Waiting", waiting.length],
          ["in", "Received"],
          ["out", "Sent"],
          ["problems", "Problems", lines.filter(problem).length],
        ]} />
        <div className="card">
          {shown.length ? (
            <div className="table-wrap">
              <table className="table log-table">
                <thead><tr><th>Time</th><th /><th>From</th><th>To</th><th className="grow">Status</th><th>Size</th></tr></thead>
                <tbody>
                  {shown.map((l) => {
                    const [tone, label] = STATUS[l.status] ?? ["", l.status];
                    return (
                      <tr key={l.key}>
                        <td className="log-time">{fmtTime(l.at)}</td>
                        <td title={l.dir === "in" ? "Received" : "Sent"}><Icon name={l.dir === "in" ? "move_to_inbox" : "send"} size={18} className="item-icon" /></td>
                        <td>{l.from || <span className="muted">None</span>}</td>
                        <td>{l.to}</td>
                        <td className="wrap">
                          <span className="log-event"><span className={"dot " + tone} />{label}</span>
                          {l.detail && <small className="row-detail">{l.detail}</small>}
                        </td>
                        <td className="muted">{l.size ? fmtSize(l.size) : ""}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          ) : <p className="empty">{queue && mail ? "Nothing here yet" : "Loading"}</p>}
        </div>
      </div>
    </>
  );
}
