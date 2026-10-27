import type { ReactNode } from "react";
import { Badge, healthText } from "../components/Badge";
import { useFloating } from "../components/Floating";
import { Icon } from "../components/Icon";
import { useModal } from "../components/Modal";
import { fmtTime, timeAgo } from "../format";
import { domainOK, port25, statusOf, useOverview } from "../overview";
import { ClientSettings } from "../setup/ClientSettings";
import { RepairCloudflare } from "../setup/RepairCloudflare";
import { useEnableTailscale } from "../setup/tailscale";
import { Wizard } from "../setup/Wizard";

function Line({ label, children }: { label: string; children: ReactNode }) {
  return <div className="row-line"><span>{label}</span><span>{children}</span></div>;
}

export function CloudflarePopover() {
  const { ov } = useOverview();
  const { close } = useFloating();
  const { open } = useModal();
  const cf = ov?.checks?.cloudflare;
  const tun = ov?.checks?.tunnel;
  const k = statusOf(ov).cf;
  return (
    <>
      <div className="pop-head"><Icon name="cloud" />Cloudflare<Badge kind={k}>{cf?.connected ? healthText[k] : "Not connected"}</Badge></div>
      <div className="pop-body">
        {cf?.connected ? <>
          <Line label="Primary domain">{cf.domain}</Line>
          <Line label="Tunnel">{cf.tunnel_status || "—"} · {cf.tunnel_connections || 0} conn</Line>
          <Line label="Connector (cloudflared)">{tun?.running ? "running" : tun?.last_error || "not running"}</Line>
          <Line label="Domains healthy">{cf.domains.filter(domainOK).length} of {cf.domains.length}</Line>
          <Line label="Inbound path">{cf.public_healthy ? "reachable" : "unreachable"}</Line>
          <Line label="Connected">{fmtTime(ov?.connected_at)}</Line>
        </> : <p className="muted">Cloudflare receives your mail and passes it to this server through a tunnel.</p>}
      </div>
      <div className="pop-foot">
        {cf?.connected
          ? <>
            <a className="btn sm" href="#domains" onClick={close}>Domains</a>
            <button className="btn sm" onClick={() => { close(); open(<RepairCloudflare domains={ov?.domains ?? []} />); }}><Icon name="refresh" />Repair</button>
          </>
          : <button className="btn sm primary" onClick={() => { close(); open(<Wizard />); }}>Set up</button>}
      </div>
    </>
  );
}

export function TailscalePopover() {
  const { ov } = useOverview();
  const { close } = useFloating();
  const enable = useEnableTailscale();
  const ts = ov?.checks?.tailscale;
  const k = statusOf(ov).ts;
  return (
    <>
      <div className="pop-head"><Icon name="globe" />Tailscale<Badge kind={k}>{healthText[k]}</Badge></div>
      <div className="pop-body">
        <Line label="Installed">{ts?.installed ? "yes" : "no"}</Line>
        <Line label="Signed in">{ts?.running ? "yes" : ts?.state || "no"}</Line>
        {ts?.dns_name && <Line label="Address">{ts.dns_name}</Line>}
        <Line label="Funnel · IMAP :443">{ts?.funnel_imap ? "on" : "off"}</Line>
        <Line label="Funnel · SMTP :8443">{ts?.funnel_submission ? "on" : "off"}</Line>
      </div>
      <div className="pop-foot">
        {!ts?.installed
          ? <a className="btn sm" href="https://tailscale.com/download" target="_blank" rel="noopener"><Icon name="external" />Download</a>
          : <button className="btn sm" onClick={() => { close(); enable(); }}><Icon name="refresh" />{ts.funnel_imap ? "Re-apply" : "Enable"}</button>}
      </div>
    </>
  );
}

export function MailFlowPopover() {
  const { ov } = useOverview();
  const { close } = useFloating();
  const p25 = port25(ov);
  const k = statusOf(ov).out;
  return (
    <>
      <div className="pop-head"><Icon name="send" />Mail flow<Badge kind={k}>{healthText[k]}</Badge></div>
      <div className="pop-body">
        <Line label="Outbound port 25">{p25 ? (p25.ok ? "open" : "blocked") : "—"}</Line>
        <Line label="Waiting to send">{ov?.queue ?? 0}</Line>
        <Line label="Last received">{timeAgo(ov?.last_inbound)}</Line>
      </div>
      <div className="pop-foot"><a className="btn sm" href="#queue" onClick={close}>View queue</a></div>
    </>
  );
}

export function ClientPopover() {
  return (
    <>
      <div className="pop-head"><Icon name="phone" />Outlook / Gmail app</div>
      <div className="pop-body"><ClientSettings /></div>
    </>
  );
}
