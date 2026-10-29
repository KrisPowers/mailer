import { useState } from "react";
import { api } from "../api";
import { fmtSize, fmtTime } from "../format";
import type { FlagReason, FlaggedMessage } from "../types";
import { IconButton } from "../ui/Button";
import { Chip, EmptyRow, Tabs } from "../ui/Page";
import { useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

type Filter = "all" | FlagReason;

const REASON: Record<FlagReason, ["plain" | "bad" | "warn", string]> = {
  flagged: ["plain", "Flagged"],
  risky: ["bad", "Risky file"],
  marketing: ["warn", "Marketing"],
};

// Flagged is every message flagged in a mail app, or by Mailer on delivery
// for an attachment that can run code or for bulk mail, in every account.
export function Flagged() {
  const list = useLoad(api.flagged);
  const [filter, setFilter] = useState<Filter>("all");
  const toast = useToast();
  const all = list.data ?? [];
  const count = (r: FlagReason) => all.filter((m) => m.reasons.includes(r)).length;
  const rows = filter === "all" ? all : all.filter((m) => m.reasons.includes(filter));

  const act = async (m: FlaggedMessage, run: typeof api.clearFlag, done: string) => {
    await run(m);
    toast(done);
    list.reload();
  };

  return (
    <>
      <Tabs<Filter> value={filter} onChange={setFilter} tabs={[
        ["all", `All (${all.length})`],
        ["flagged", `Flagged by users (${count("flagged")})`],
        ["risky", `Risky files (${count("risky")})`],
        ["marketing", `Marketing (${count("marketing")})`],
      ]} />
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th>Why</th><th>Time</th><th>From</th><th className="grow">Subject</th><th>Account</th><th>Folder</th>
              <th className="num">Size</th><th className="col-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            {list.error ? <EmptyRow cols={8} icon="error">{list.error}</EmptyRow>
              : !list.data ? <EmptyRow cols={8}>Loading…</EmptyRow>
              : !rows.length ? <EmptyRow cols={8} icon="flag">Nothing flagged</EmptyRow>
              : rows.map((m) => (
                <tr key={m.account + m.mailbox + m.uid}>
                  <td>
                    <span className="chips">
                      {m.reasons.map((r) => (
                        <span key={r} data-tip={r === "risky" && m.files?.length ? m.files.join(", ") : undefined}>
                          <Chip health={REASON[r][0]}>{REASON[r][1]}</Chip>
                        </span>
                      ))}
                    </span>
                  </td>
                  <td>{fmtTime(m.date)}</td>
                  <td>{m.from || <span className="muted">(none)</span>}</td>
                  <td className="wrap">{m.subject || <span className="muted">(no subject)</span>}</td>
                  <td>{m.account}</td>
                  <td>{m.mailbox}</td>
                  <td className="num">{fmtSize(m.size)}</td>
                  <td className="col-actions">
                    <span className="row-actions">
                      <IconButton icon="close" label="Clear flag" onClick={() => act(m, api.clearFlag, "Flag cleared")} />
                      <IconButton icon="delete" label="Move to Trash" tone="danger" onClick={() => act(m, api.trashFlagged, "Moved to Trash")} />
                    </span>
                  </td>
                </tr>
              ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
