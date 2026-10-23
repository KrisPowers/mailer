import { useCallback } from "react";
import { api } from "../api";
import { Icon } from "../components/Icon";
import { CloseButton, Modal, useModal } from "../components/Modal";
import { useToast } from "../components/Toasts";
import { AsyncButton } from "../components/AsyncButton";
import { useOverview } from "../overview";
import type { TailscaleResult } from "../types";

// useEnableTailscale turns on Funnel. Unless quiet, it reports the outcome
// itself, including a dialog when Tailscale wants a one-time approval.
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
      else if (r.action_url) open(<ApprovalModal result={r} retry={() => enable()} />);
      else toast(r.error || "Couldn't enable Funnel", true);
    }
    return r;
  }, [reload, toast, open]);
  return enable;
}

function ApprovalModal({ result, retry }: { result: TailscaleResult; retry: () => Promise<unknown> }) {
  const { close } = useModal();
  return (
    <Modal
      title="Tailscale needs approval"
      footer={<>
        <CloseButton />
        <AsyncButton className="btn" onClick={async () => { close(); await retry(); }}>Try again</AsyncButton>
      </>}
    >
      <p>{result.error}</p>
      <a className="btn primary" href={result.action_url} target="_blank" rel="noopener">
        <Icon name="external" />Open Tailscale
      </a>
    </Modal>
  );
}
