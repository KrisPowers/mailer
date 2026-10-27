import { useEffect } from "react";
import { api } from "../api";
import { Badge } from "../components/Badge";
import { Icon } from "../components/Icon";
import { Kpi, PageHead, Panel } from "../components/Layout";
import { fmtSize, timeAgo } from "../format";
import { useOverview } from "../overview";
import { ClientSettings } from "../setup/ClientSettings";
import { useLoad } from "../useLoad";

const LOG_LINE = /^(\d{4}\/\d\d\/\d\d) (\d\d:\d\d:\d\d) (.*)$/;

export function Dashboard() {
  const { ov } = useOverview();
  const accounts = useLoad(api.accounts);
  const logs = useLoad(api.logs);

  // Follow the overview's 20s refresh so the page stays current.
  useEffect(() => {
    accounts.reload();
    logs.reload();
  }, [ov]);

  const list = accounts.data ?? [];
  const totals = list.reduce((t, a) => ({ msgs: t.msgs + a.messages, unseen: t.unseen + a.unseen, size: t.size + a.size }),
    { msgs: 0, unseen: 0, size: 0 });
  const items = ov?.checks?.items ?? [];
  const recent = (logs.data ?? []).slice(-10).reverse();

  return (
    <>
      <PageHead title="Dashboard" sub={ov?.domain} />
      <div className="kpis">
        <Kpi icon="users" value={list.length} label="Accounts" />
        <Kpi icon="inbox" value={totals.msgs.toLocaleString()} label={"Messages · " + fmtSize(totals.size)} />
        <Kpi icon="mail" value={totals.unseen.toLocaleString()} label="Unread in inboxes" />
        <Kpi icon="send" value={ov?.queue ?? 0} label="Waiting to send" />
        <Kpi icon="clock" value={timeAgo(ov?.last_inbound)} label="Last received" />
      </div>
      <div className="grid-2">
        <div>
          <Panel title="System status" icon="activity"
            actions={<span className="muted small">{ov?.checks ? "Checked " + timeAgo(ov.checks.at) : ""}</span>}>
            <div className="table-wrap">
              <table className="status-table">
                <tbody>
                  {items.length ? items.map((i) => (
                    <tr key={i.name}>
                      <td><Icon name={i.ok ? "check" : "alert"} /></td>
                      <td className="wrap">{i.name}<div className="sub-detail">{i.detail}</div></td>
                      <td><Badge kind={i.ok ? "ok" : "bad"}>{i.ok ? "OK" : "Problem"}</Badge></td>
                      <td className="detail">{i.detail}{i.fix && <div className="fix">{i.fix}</div>}</td>
                    </tr>
                  )) : <tr><td colSpan={4} className="empty">Checking…</td></tr>}
                </tbody>
              </table>
            </div>
          </Panel>
        </div>
        <div>
          <Panel title="Client settings" icon="phone">
            <div className="panel-body"><ClientSettings /></div>
          </Panel>
          <Panel title="Recent activity" icon="terminal" actions={<a className="btn sm" href="#logs">All logs</a>}>
            {recent.length ? (
              <ul className="activity">
                {recent.map((l, i) => {
                  const m = l.match(LOG_LINE);
                  return <li key={i}><time>{m ? m[2] : ""}</time><span>{m ? m[3] : l}</span></li>;
                })}
              </ul>
            ) : <div className="empty">No activity yet.</div>}
          </Panel>
        </div>
      </div>
    </>
  );
}
