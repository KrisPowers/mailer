import { useState } from "react";
import { api } from "../api";
import { fmtTime } from "../format";
import { useOverview, type Health } from "../overview";
import { Button } from "../ui/Button";
import { SearchField } from "../ui/Field";
import { PopoverButton } from "../ui/Floating";
import { Icon } from "../ui/Icon";
import { Dot, EmptyRow, Metrics, PageHeader, Showing, Toolbar } from "../ui/Page";
import { Toggle } from "../ui/Toggle";
import { useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

const STATUS: Record<string, [Health, string]> = { pending: ["warn", "Waiting"], sent: ["ok", "Sent"], failed: ["bad", "Failed"] };

export function Queue() {
  const { data, error, reload } = useLoad(api.queue);
  const { reload: reloadOverview } = useOverview();
  const toast = useToast();
  const [q, setQ] = useState("");
  const [failedOnly, setFailedOnly] = useState(false);

  const rows = (data ?? []).flatMap((it) => it.rcpts.map((r) => ({ it, r })));
  const list = rows.filter(({ it, r }) => (!failedOnly || r.status === "failed") && (it.from + " " + r.addr).toLowerCase().includes(q.toLowerCase()));

  return (
    <>
      <PageHeader title="Outbound queue" actions={
        <Button icon="restart_alt" disabled={!rows.length} onClick={async () => {
          await api.retryQueue();
          toast("Retrying now");
          setTimeout(() => { reload(); reloadOverview(); }, 2500);
        }}>Retry all</Button>
      } />
      <Metrics items={[
        { label: "Waiting", value: rows.filter(({ r }) => r.status === "pending").length, tip: "Retried for 4 days, then bounced to the sender" },
        { label: "Failed", value: rows.filter(({ r }) => r.status === "failed").length },
      ]} />
      <Toolbar>
        <SearchField value={q} onChange={setQ} />
        <Toggle checked={failedOnly} onChange={setFailedOnly}>Only show failed</Toggle>
      </Toolbar>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr><th className="col-dot" /><th>From</th><th>To</th><th>Status</th><th className="num">Attempts</th><th>Queued</th><th className="grow">Next try</th><th className="col-actions">Actions</th></tr>
          </thead>
          <tbody>
            {error ? <EmptyRow cols={8} icon="error">{error}</EmptyRow>
              : !data ? <EmptyRow cols={8}>Loading…</EmptyRow>
              : !list.length ? <EmptyRow cols={8} icon="outbox">{rows.length ? "Nothing matches." : "Nothing waiting to send."}</EmptyRow>
              : list.map(({ it, r }) => {
                const [health, label] = STATUS[r.status] ?? ["none", r.status];
                return (
                  <tr key={it.id + r.addr}>
                    <td className="col-dot"><Dot health={health} label={label} /></td>
                    <td>{it.from}</td>
                    <td>{r.addr}</td>
                    <td>{label}</td>
                    <td className="num">{it.attempts}</td>
                    <td>{fmtTime(it.created)}</td>
                    <td>{r.status === "pending" ? fmtTime(it.next_try) : "-"}</td>
                    <td className="col-actions">
                      {r.last_error && (
                        <PopoverButton label="Last error" content={<div className="popover-body"><h3>Last error</h3><p className="mono">{r.last_error}</p></div>}>
                          <Icon name="info" />
                        </PopoverButton>
                      )}
                    </td>
                  </tr>
                );
              })}
          </tbody>
        </table>
      </div>
      {data && <Showing count={list.length} />}
    </>
  );
}
