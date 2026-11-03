import { useState } from "react";
import { fmtTime, message } from "../../shared/format";
import { Icon } from "../../shared/Icon";
import { Card, PageHead, Tally, useNotify } from "../../shared/ui";
import { api } from "./api";
import { health, useOverview, type Area, type Health } from "./overview";
import { Steps, TailscaleOutcome, useEnableTailscale } from "./parts";
import type { ConnectResult } from "./types";

const AREAS: { area: Area; title: string; sub: string }[] = [
  { area: "cloudflare", title: "Cloudflare", sub: "Receives mail for your domains and hands it to this server through the tunnel." },
  { area: "tailscale", title: "Tailscale Funnel", sub: "How Outlook and other mail apps reach this server." },
  { area: "sending", title: "Sending", sub: "Outgoing mail leaves through Cloudflare Email Sending." },
];

const word = (h: Health) => ({ ok: "Working", warn: "Mail waiting", bad: "Needs a look", "": "Checking" })[h];
const tone = (h: Health) => (h === "" ? undefined : h);

// useRepair re-runs the Cloudflare setup for each domain in turn, fixing
// whatever drifted. It's safe any time.
export function useRepair() {
  const { reload } = useOverview();
  const [results, setResults] = useState<[string, ConnectResult][] | null>(null);
  const [busy, setBusy] = useState(false);
  const repair = async (domains: string[]) => {
    setBusy(true);
    setResults([]);
    for (const d of domains) {
      const r = await api.connectCloudflare("", d).catch((err): ConnectResult => ({ error: message(err) }));
      setResults((prev) => [...(prev ?? []), [d, r]]);
    }
    setBusy(false);
    await reload();
  };
  return { repair, results, busy };
}

export function RepairOutcome({ results, busy }: { results: [string, ConnectResult][] | null; busy: boolean }) {
  if (!results) return null;
  return (
    <div className="card-pad">
      {results.map(([d, r]) => (
        <div key={d}>
          {results.length > 1 && <h3 className="section-title">{d}</h3>}
          <Steps steps={r.steps} error={r.error} />
        </div>
      ))}
      {busy && <p className="muted"><span className="spinner tiny" />Checking and fixing the tunnel, Worker, Email Routing, Email Sending and DNS</p>}
    </div>
  );
}

// Status is every connection check, run every minute, with what fixes each.
export function Status() {
  const { ov, reload } = useOverview();
  const notify = useNotify();
  const [checking, setChecking] = useState(false);
  const ts = useEnableTailscale();
  const fix = useRepair();
  const items = ov?.checks?.items ?? [];

  const checkNow = async () => {
    setChecking(true);
    try {
      await api.check();
      await reload();
    } catch (e) {
      notify(message(e), "bad");
    } finally {
      setChecking(false);
    }
  };

  return (
    <>
      <PageHead title="Status" sub={ov?.checks ? `Checked ${fmtTime(ov.checks.at)}, again every minute` : "Checking"}
        actions={<button type="button" className="btn" disabled={checking} onClick={checkNow}>{checking ? <span className="spinner small" /> : <Icon name="refresh" size={18} />}Check now</button>} />
      <div className="page-body stack">
        <Tally items={AREAS.map(({ area, title }) => ({ label: title, value: word(health(ov, area)), tone: tone(health(ov, area)) }))} />
        {AREAS.map(({ area, title, sub }) => {
          const list = items.filter((i) => i.area === area);
          return (
            <Card key={area} title={title} sub={sub} action={
              area === "cloudflare" ? (
                <button type="button" className="btn" disabled={fix.busy || !ov?.cloudflare_setup} onClick={() => fix.repair(ov?.domains ?? [])}><Icon name="build" size={18} />Repair</button>
              ) : area === "tailscale" ? (
                <button type="button" className="btn" disabled={ts.busy} onClick={ts.enable}><Icon name="lan" size={18} />{ts.busy ? "Enabling" : "Enable Funnel"}</button>
              ) : undefined
            }>
              {list.length ? (
                <ul className="check-list card-pad">
                  {list.map((c) => (
                    <li key={c.name} className="check-row">
                      <span className={"dot " + (c.ok ? "ok" : "bad")} />
                      <span className="check-name">{c.name}</span>
                      <span className="check-detail">{c.detail}{c.fix && <small>{c.fix}</small>}</span>
                    </li>
                  ))}
                </ul>
              ) : <p className="empty-note card-pad">{ov?.checks ? "Nothing to check yet" : "Checking"}</p>}
              {area === "cloudflare" && <RepairOutcome results={fix.results} busy={fix.busy} />}
              {area === "tailscale" && ts.result && <div className="card-pad"><TailscaleOutcome result={ts.result} /></div>}
            </Card>
          );
        })}
      </div>
    </>
  );
}
