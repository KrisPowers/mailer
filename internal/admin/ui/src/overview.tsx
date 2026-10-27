import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { api } from "./api";
import type { Health } from "./components/Badge";
import type { DomainStatus, Overview } from "./types";

interface OverviewState {
  ov: Overview | null;
  error: string;
  reload: () => Promise<Overview | null>;
}

const OverviewContext = createContext<OverviewState>({ ov: null, error: "", reload: async () => null });

export const useOverview = () => useContext(OverviewContext);

// OverviewProvider keeps the server summary fresh: on load, every 20s and
// whenever a page calls reload after changing something.
export function OverviewProvider({ children }: { children: ReactNode }) {
  const [ov, setOv] = useState<Overview | null>(null);
  const [error, setError] = useState("");

  const reload = useCallback(async () => {
    try {
      const o = await api.overview();
      setOv(o);
      setError("");
      return o;
    } catch (err) {
      setError((err as Error).message);
      return null;
    }
  }, []);

  useEffect(() => {
    reload();
    const t = setInterval(reload, 20000);
    return () => clearInterval(t);
  }, [reload]);

  return <OverviewContext.Provider value={{ ov, error, reload }}>{children}</OverviewContext.Provider>;
}

export interface StatusSummary {
  cf: Health;
  ts: Health;
  out: Health;
}

export function statusOf(ov: Overview | null): StatusSummary {
  const c = ov?.checks;
  if (!ov || !c) return { cf: "none", ts: "none", out: "none" };
  const cf = c.cloudflare;
  const ts = c.tailscale;
  const p25 = port25(ov);
  return {
    cf: !cf.connected ? "bad"
      : cf.tunnel_status === "healthy" && cf.public_healthy && cf.domains.every(domainOK) ? "ok" : "warn",
    ts: !ts.installed || !ts.running ? "bad" : ts.funnel_imap && ts.funnel_submission ? "ok" : "warn",
    out: !p25 ? "none" : !p25.ok ? "bad" : ov.queue ? "warn" : "ok",
  };
}

export function domainOK(d: DomainStatus): boolean {
  return !d.error && d.checks.length > 0 && d.checks.every((c) => c.ok);
}

export function port25(ov: Overview | null) {
  return ov?.checks?.items.find((i) => i.name.startsWith("Outbound port 25"));
}

// setupStep is the first unfinished setup step: 1 Cloudflare, 2 Tailscale,
// 3 first account, or 0 when everything is done.
export function setupStep(ov: Overview | null): 0 | 1 | 2 | 3 {
  if (!ov) return 0;
  if (!ov.cloudflare_setup) return 1;
  if (!ov.tailscale_setup) return 2;
  if (!ov.accounts) return 3;
  return 0;
}
