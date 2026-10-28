import { useCallback } from "react";
import { api } from "../api";
import { useOverview } from "../overview";
import type { TailscaleResult } from "../types";
import { Button } from "../ui/Button";
import { Icon } from "../ui/Icon";
import { Modal, useModal } from "../ui/Modal";
import { useToast } from "../ui/Toast";

// useEnableTailscale turns on Funnel. Unless quiet, it reports the outcome
// itself, with a dialog when Tailscale wants a sign-in or an approval.
export function useEnableTailscale() {
  const { reload } = useOverview();
  const toast = useToast();
  const { open } = useModal();
  const enable = useCallback(async (quiet = false): Promise<TailscaleResult> => {
    let r: TailscaleResult;
    try {
      r = await api.enableTailscale();
    } catch (err) {
      r = { error: (err as Error).message };
    }
    await reload();
    if (!quiet) {
      if (r.ok) toast("Funnel enabled on " + r.dns_name);
      else if (r.action_url) open(<Approval result={r} retry={() => enable()} />);
      else toast(r.error || "Couldn't enable Funnel", "bad");
    }
    return r;
  }, [reload, toast, open]);
  return enable;
}

function Approval({ result, retry }: { result: TailscaleResult; retry: () => Promise<unknown> }) {
  const { close } = useModal();
  return (
    <Modal title="Tailscale needs you" actions={<>
      <Button variant="text" onClick={close}>Cancel</Button>
      <Button variant="primary" onClick={async () => { close(); await retry(); }}>I've done it, try again</Button>
    </>}>
      <p>{result.error}</p>
      <a className="btn secondary" href={result.action_url} target="_blank" rel="noopener noreferrer">
        <Icon name="open_in_new" size={18} /><span>Open Tailscale</span>
      </a>
    </Modal>
  );
}
