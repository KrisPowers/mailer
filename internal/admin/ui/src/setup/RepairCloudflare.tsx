import { useEffect, useState } from "react";
import { api } from "../api";
import { CloseButton, Modal } from "../components/Modal";
import { useToast } from "../components/Toasts";
import { useOverview } from "../overview";
import type { ConnectResult } from "../types";
import { Progress } from "./Progress";

// RepairCloudflare re-runs the idempotent Cloudflare setup with the saved
// token for each domain in turn, fixing anything that drifted.
export function RepairCloudflare({ domains, onDone }: { domains: string[]; onDone?: () => void }) {
  const { reload } = useOverview();
  const toast = useToast();
  const [results, setResults] = useState<[string, ConnectResult][]>([]);
  const [running, setRunning] = useState(true);

  useEffect(() => {
    let live = true;
    (async () => {
      let failed = false;
      for (const d of domains) {
        const r = await api.connectCloudflare("", d)
          .catch((err) => ({ error: (err as Error).message }) as ConnectResult);
        if (!live) return;
        failed ||= !!r.error;
        setResults((prev) => [...prev, [d, r]]);
      }
      setRunning(false);
      if (!failed) toast("Cloudflare is configured");
      reload();
      onDone?.();
    })();
    return () => { live = false; };
    // Run once when the dialog opens.
  }, []);

  return (
    <Modal title="Repair Cloudflare setup" footer={<CloseButton />}>
      {results.map(([d, r]) => (
        <div key={d}>
          {domains.length > 1 && <b>{d}</b>}
          <Progress steps={r.steps} error={r.error} />
        </div>
      ))}
      {running && <p className="muted">Re-checking and fixing tunnel, Worker, Email Routing and DNS…</p>}
    </Modal>
  );
}
