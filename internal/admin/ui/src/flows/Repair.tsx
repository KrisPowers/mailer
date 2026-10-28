import { useEffect, useState } from "react";
import { api } from "../api";
import { useOverview } from "../overview";
import type { ConnectResult } from "../types";
import { Modal } from "../ui/Modal";
import { Steps } from "../ui/Page";
import { useToast } from "../ui/Toast";

// Repair re-runs the Cloudflare setup for each domain in turn, fixing
// whatever drifted. It's safe to run any time.
export function Repair({ domains, onDone }: { domains: string[]; onDone?: () => void }) {
  const { reload } = useOverview();
  const toast = useToast();
  const [results, setResults] = useState<[string, ConnectResult][]>([]);
  const [running, setRunning] = useState(true);

  useEffect(() => {
    let live = true;
    (async () => {
      let failed = false;
      for (const d of domains) {
        const r = await api.connectCloudflare("", d).catch((err) => ({ error: (err as Error).message }) as ConnectResult);
        if (!live) return;
        failed ||= !!r.error;
        setResults((prev) => [...prev, [d, r]]);
      }
      setRunning(false);
      if (!failed) toast("Cloudflare setup re-applied");
      reload();
      onDone?.();
    })();
    return () => { live = false; };
    // Runs once, when the dialog opens.
  }, []);

  return (
    <Modal title={"Repair " + (domains.length === 1 ? domains[0] : "all domains")}>
      {results.map(([d, r]) => (
        <div key={d}>
          {domains.length > 1 && <h3 className="sub-title">{d}</h3>}
          <Steps steps={r.steps} error={r.error} />
        </div>
      ))}
      {running && <p className="muted"><span className="spinner" /> Checking and fixing the tunnel, Worker, Email Routing, Email Sending and DNS…</p>}
    </Modal>
  );
}
