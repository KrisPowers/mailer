import { api } from "../api";
import { AsyncButton } from "../components/AsyncButton";
import { Badge } from "../components/Badge";
import { PopoverButton } from "../components/Floating";
import { Icon } from "../components/Icon";
import { EmptyRow, PageHead, Panel } from "../components/Layout";
import { useToast } from "../components/Toasts";
import { fmtTime } from "../format";
import { useOverview } from "../overview";
import { useLoad } from "../useLoad";

export function Queue() {
  const { data, error, reload } = useLoad(api.queue);
  const { reload: reloadOverview } = useOverview();
  const toast = useToast();

  const rows = (data ?? []).flatMap((it) => it.rcpts.map((r) => ({ it, r })));

  const retry = async () => {
    await api.retryQueue();
    toast("Retrying now");
    setTimeout(() => { reload(); reloadOverview(); }, 2500);
  };

  return (
    <>
      <PageHead title="Outbound queue" sub="Retries for 4 days, then bounces to the sender">
        <AsyncButton className="btn" disabled={!rows.length} onClick={retry}><Icon name="refresh" />Retry all</AsyncButton>
      </PageHead>
      <Panel>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Queued</th><th>From</th><th>To</th><th>Status</th>
                <th className="num">Attempts</th><th>Next try</th><th />
              </tr>
            </thead>
            <tbody>
              {error ? <EmptyRow cols={7}>{error}</EmptyRow>
                : !data ? <EmptyRow cols={7}>Loading…</EmptyRow>
                : !rows.length ? <EmptyRow cols={7}>Nothing waiting to send.</EmptyRow>
                : rows.map(({ it, r }) => (
                  <tr key={it.id + r.addr}>
                    <td>{fmtTime(it.created)}</td>
                    <td>{it.from}</td>
                    <td>{r.addr}</td>
                    <td><Badge kind={r.status === "sent" ? "ok" : r.status === "failed" ? "bad" : "warn"}>{r.status}</Badge></td>
                    <td className="num">{it.attempts}</td>
                    <td>{r.status === "pending" ? fmtTime(it.next_try) : "—"}</td>
                    <td className="act">
                      {r.last_error && (
                        <PopoverButton title="Last error" content={<>
                          <div className="pop-head"><Icon name="alert" />Last error</div>
                          <div className="pop-body mono small">{r.last_error}</div>
                        </>}>
                          <Icon name="info" />
                        </PopoverButton>
                      )}
                    </td>
                  </tr>
                ))}
            </tbody>
          </table>
        </div>
      </Panel>
    </>
  );
}
