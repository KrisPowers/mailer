import { Badge, healthText } from "../components/Badge";
import { useFloating } from "../components/Floating";
import { Icon, type IconName } from "../components/Icon";
import { useModal } from "../components/Modal";
import { timeAgo } from "../format";
import { statusOf, useOverview, type Area } from "../overview";
import { ClientSettings } from "../setup/ClientSettings";
import { RepairCloudflare } from "../setup/RepairCloudflare";
import { useEnableTailscale } from "../setup/tailscale";
import { Wizard } from "../setup/Wizard";
import type { ReactNode } from "react";

// AreaPopover lists every check behind one status icon: what was checked,
// what came back, and for a failure, what to do about it.
function AreaPopover({ area, icon, title, foot, children }: {
  area: Area;
  icon: IconName;
  title: string;
  foot: ReactNode;
  children?: ReactNode;
}) {
  const { ov } = useOverview();
  const k = statusOf(ov)[area === "cloudflare" ? "cf" : area === "tailscale" ? "ts" : "out"];
  const items = (ov?.checks?.items ?? []).filter((i) => i.area === area);
  return (
    <>
      <div className="pop-head"><Icon name={icon} />{title}<Badge kind={k}>{healthText[k]}</Badge></div>
      <div className="pop-body">
        {items.length ? (
          <ul className="checks">
            {items.map((i) => (
              <li key={i.name} className={i.ok ? "ok" : "bad"}>
                <Icon name={i.ok ? "check" : "alert"} />
                <b>{i.name}</b>
                <span className="d">{i.detail}</span>
                {i.fix && <span className="fix">{i.fix}</span>}
              </li>
            ))}
          </ul>
        ) : <p className="muted">Checking…</p>}
        {children}
      </div>
      <div className="pop-foot">{foot}</div>
    </>
  );
}

export function CloudflarePopover() {
  const { ov } = useOverview();
  const { close } = useFloating();
  const { open } = useModal();
  return (
    <AreaPopover area="cloudflare" icon="cloud" title="Cloudflare: receiving mail" foot={ov?.cloudflare_setup ? <>
      <a className="btn sm" href="#domains" onClick={close}>Domains</a>
      <button className="btn sm primary" onClick={() => { close(); open(<RepairCloudflare domains={ov?.domains ?? []} />); }}><Icon name="refresh" />Repair</button>
    </> : <button className="btn sm primary" onClick={() => { close(); open(<Wizard step={1} />); }}>Connect Cloudflare</button>} />
  );
}

export function TailscalePopover() {
  const { ov } = useOverview();
  const { close } = useFloating();
  const enable = useEnableTailscale();
  const ts = ov?.checks?.tailscale;
  return (
    <AreaPopover area="tailscale" icon="globe" title="Tailscale: mail apps" foot={!ts?.installed
      ? <a className="btn sm primary" href="https://tailscale.com/download" target="_blank" rel="noopener"><Icon name="external" />Download Tailscale</a>
      : <button className="btn sm primary" onClick={() => { close(); enable(); }}><Icon name="refresh" />Enable Funnel</button>} />
  );
}

export function MailFlowPopover() {
  const { ov } = useOverview();
  const { close } = useFloating();
  return (
    <AreaPopover area="sending" icon="send" title="Sending mail" foot={<>
      <a className="btn sm" href="#settings" onClick={close}>Settings</a>
      <a className="btn sm" href="#queue" onClick={close}>View queue</a>
    </>}>
      <p className="muted small">
        {ov?.queue ? `${ov.queue} message(s) waiting to send.` : "Nothing waiting to send."} Last received {timeAgo(ov?.last_inbound)}.
      </p>
    </AreaPopover>
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
