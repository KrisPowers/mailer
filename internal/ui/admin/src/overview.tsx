import { createContext, useCallback, useContext, useEffect, useState, type ReactNode } from "react";
import { message } from "../../shared/format";
import { api } from "./api";
import type { Check, Overview } from "./types";

// Health is how a status reads: fine, needs a look, failing, or not known yet.
export type Health = "ok" | "warn" | "bad" | "";

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
      setError(message(err));
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

export type Area = Check["area"];

// health reads an area's checks: failing when any failed, waiting when
// sending works but mail sits in the queue.
export function health(ov: Overview | null, area: Area): Health {
  const items = ov?.checks?.items?.filter((i) => i.area === area);
  if (!items?.length) return "";
  if (!items.every((i) => i.ok)) return "bad";
  return area === "sending" && ov?.queue ? "warn" : "ok";
}

export const failing = (ov: Overview | null) => (ov?.checks?.items ?? []).filter((i) => !i.ok);

// setupStep is the first unfinished setup step: 1 Cloudflare, 2 Tailscale,
// 3 first account, or 0 when everything is done.
export function setupStep(ov: Overview | null): 0 | 1 | 2 | 3 {
  if (!ov) return 0;
  if (!ov.cloudflare_setup) return 1;
  if (!ov.tailscale_setup) return 2;
  if (!ov.accounts) return 3;
  return 0;
}
