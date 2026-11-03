import { useCallback, useEffect, useState } from "react";
import { fmtSize, fmtTime, message } from "../../shared/format";
import { Icon } from "../../shared/Icon";
import { Filters, PageHead, useNotify } from "../../shared/ui";
import { api } from "./api";
import { useOverview } from "./overview";
import { accountHref } from "./route";
import type { FlagReason, FlaggedMessage } from "./types";

type Show = "all" | FlagReason;

const REASON: Record<FlagReason, [string, string]> = {
  flagged: ["plain", "Flagged"],
  risky: ["bad", "Risky file"],
  marketing: ["warn", "Marketing"],
};

// Flagged is every message flagged by its reader, or by Mailer on delivery for
// an attachment that can run code or for bulk mail, in every account.
export function Flagged() {
  const [list, setList] = useState<FlaggedMessage[] | null>(null);
  const [show, setShow] = useState<Show>("all");
  const { reload } = useOverview();
  const notify = useNotify();
  const load = useCallback(() => api.flagged().then(setList).catch((e) => notify(message(e), "bad")), [notify]);
  useEffect(() => { load(); }, [load]);

  const all = list ?? [];
  const count = (r: FlagReason) => all.filter((m) => m.reasons.includes(r)).length;
  const rows = show === "all" ? all : all.filter((m) => m.reasons.includes(show));
  const act = async (m: FlaggedMessage, run: (m: FlaggedMessage) => Promise<unknown>, done: string) => {
    try {
      await run(m);
      notify(done);
      await Promise.all([load(), reload()]);
    } catch (e) {
      notify(message(e), "bad");
    }
  };

  return (
    <>
      <PageHead title="Flagged" sub="Flagged by readers, attachments that can run code, and bulk mail, in every account" />
      <div className="page-body stack">
        <Filters<Show> value={show} onChange={setShow} items={[
          ["all", "All", all.length],
          ["flagged", "Flagged by readers", count("flagged")],
          ["risky", "Risky files", count("risky")],
          ["marketing", "Marketing", count("marketing")],
        ]} />
        <div className="card">
          {rows.length ? (
            <div className="table-wrap">
              <table className="table">
                <thead><tr><th>Why</th><th>Time</th><th>From</th><th className="grow">Subject</th><th>Account</th><th>Size</th><th /></tr></thead>
                <tbody>
                  {rows.map((m) => (
                    <tr key={m.account + m.mailbox + m.uid}>
                      <td>
                        <span className="chips">
                          {m.reasons.map((r) => (
                            <span key={r} className={"chip " + REASON[r][0]} title={r === "risky" && m.files?.length ? m.files.join(", ") : undefined}>{REASON[r][1]}</span>
                          ))}
                        </span>
                      </td>
                      <td className="log-time">{fmtTime(m.date)}</td>
                      <td>{m.from || <span className="muted">None</span>}</td>
                      <td className="wrap">{m.subject || <span className="muted">No subject</span>}</td>
                      <td><a href={accountHref(m.account)}>{m.account}</a><small className="row-detail">{m.mailbox}</small></td>
                      <td className="muted">{fmtSize(m.size)}</td>
                      <td>
                        <span className="row-actions">
                          <button type="button" className="icon-btn small" aria-label="Clear the flag" title="Clear the flag" onClick={() => act(m, api.clearFlag, "Flag cleared")}>
                            <Icon name="close" size={18} />
                          </button>
                          <button type="button" className="icon-btn small danger" aria-label="Move to Trash" title="Move to Trash" onClick={() => act(m, api.trashFlagged, "Moved to Trash")}>
                            <Icon name="delete" size={18} />
                          </button>
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : <p className="empty">{list ? "Nothing flagged" : "Loading"}</p>}
        </div>
      </div>
    </>
  );
}
