import { useEffect, useState } from "react";
import { api } from "../api";
import { CloseButton, Modal } from "../components/Modal";
import { useToast } from "../components/Toasts";
import { useOverview } from "../overview";
import type { ConnectResult } from "../types";
import { Progress } from "./Progress";

// RepairCloudflare re-runs the idempotent Cloudflare setup with the saved
// token, fixing anything that drifted.
export function RepairCloudflare() {
  const { ov, reload } = useOverview();
  const toast = useToast();
  const [result, setResult] = useState<ConnectResult | null>(null);

  useEffect(() => {
    let live = true;
    api.connectCloudflare("", ov?.domain ?? "")
      .catch((err) => ({ error: (err as Error).message }) as ConnectResult)
      .then((r) => {
        if (!live) return;
        setResult(r);
        if (!r.error) toast("Cloudflare is configured");
        reload();
      });
    return () => { live = false; };
    // Run once when the dialog opens.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <Modal title="Repair Cloudflare setup" footer={<CloseButton />}>
      {result
        ? <Progress steps={result.steps} error={result.error} />
        : <p className="muted">Re-checking and fixing tunnel, Worker, Email Routing and DNS…</p>}
    </Modal>
  );
}
