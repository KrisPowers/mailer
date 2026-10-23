import { useState } from "react";
import { api } from "../api";
import { PopoverButton } from "../components/Floating";
import { Icon } from "../components/Icon";
import { useToast } from "../components/Toasts";
import { statusOf, useOverview } from "../overview";
import { ClientPopover, CloudflarePopover, MailFlowPopover, TailscalePopover } from "./StatusPopovers";

export function Topbar({ onToggleNav }: { onToggleNav: () => void }) {
  const { ov, reload } = useOverview();
  const toast = useToast();
  const [checking, setChecking] = useState(false);
  const st = statusOf(ov);
  const cls = (k: string) => "status-btn" + (k === "none" ? "" : " " + k);

  const recheck = async () => {
    setChecking(true);
    try {
      await api.check();
      await reload();
      toast("Connections re-checked");
    } catch (err) {
      toast((err as Error).message, true);
    } finally {
      setChecking(false);
    }
  };

  return (
    <header className="topbar">
      <button className="icon-btn nav-toggle" title="Toggle menu" onClick={onToggleNav}><Icon name="menu" /></button>
      <div className="brand"><Icon name="mail" /><span>Mailer</span></div>
      <span className="chip">{ov?.domain || "not set up"}</span>
      <div className="top-right">
        <PopoverButton className={cls(st.cf)} title="Cloudflare" content={<CloudflarePopover />}>
          <Icon name="cloud" /><span className="dot" />
        </PopoverButton>
        <PopoverButton className={cls(st.ts)} title="Tailscale" content={<TailscalePopover />}>
          <Icon name="globe" /><span className="dot" />
        </PopoverButton>
        <PopoverButton className={cls(st.out)} title="Mail flow" content={<MailFlowPopover />}>
          <Icon name="send" /><span className="dot" />
        </PopoverButton>
        <span className="divider" />
        <PopoverButton title="Outlook / Gmail settings" content={<ClientPopover />}>
          <Icon name="phone" />
        </PopoverButton>
        <button className={"icon-btn" + (checking ? " spin" : "")} title="Re-check connections" disabled={checking} onClick={recheck}>
          <Icon name="refresh" />
        </button>
      </div>
    </header>
  );
}
