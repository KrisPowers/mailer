import { useEffect, useState, type ReactNode } from "react";
import { api } from "../api";
import { Badge, healthText, type Health } from "../components/Badge";
import { MoreMenu } from "../components/Floating";
import { Icon, type IconName } from "../components/Icon";
import { PageHead, Panel } from "../components/Layout";
import { useModal } from "../components/Modal";
import { Switch } from "../components/Switch";
import { useToast } from "../components/Toasts";
import { fmtTime, plural } from "../format";
import { statusOf, useOverview } from "../overview";
import { RepairCloudflare } from "../setup/RepairCloudflare";
import { useEnableTailscale } from "../setup/tailscale";
import { Wizard } from "../setup/Wizard";
import type { Settings as SettingsT } from "../types";
import { useLoad } from "../useLoad";

export function Settings() {
  const { ov } = useOverview();
  const { data, error } = useLoad(api.settings);
  const [s, setS] = useState<SettingsT | null>(null);
  const [maxMB, setMaxMB] = useState("");
  const [token, setToken] = useState("");
  const toast = useToast();
  const { open } = useModal();
  const enableTailscale = useEnableTailscale();

  useEffect(() => {
    if (data) {
      setS(data);
      setMaxMB(String(data.max_message_mb));
      setToken(data.smtp_token);
    }
  }, [data]);

  // put saves immediately and rolls the switch back if the server refuses.
  const put = async (patch: Partial<SettingsT>) => {
    if (!s) return;
    const prev = s;
    const next = { ...s, ...patch };
    setS(next);
    try {
      await api.saveSettings(next);
      toast("Settings saved");
    } catch (err) {
      setS(prev);
      setMaxMB(String(prev.max_message_mb));
      setToken(prev.smtp_token);
      toast((err as Error).message, true);
    }
  };

  const cf = ov?.checks?.cloudflare;
  const ts = ov?.checks?.tailscale;
  const st = statusOf(ov);

  return (
    <>
      <PageHead title="Settings" />
      <Panel title="Mail handling" icon="mail">
        <div className="panel-body">
          {error && <p className="muted">{error}</p>}
          {s && <>
            <Switch title="Reject DMARC failures" checked={s.reject_dmarc_failures} onChange={(v) => put({ reject_dmarc_failures: v })}>
              Refuse mail that fails the sender's DMARC policy when it says reject. When off, it goes to Junk.
            </Switch>
            <div className="switch-row">
              <div className="txt">
                <b>Cloudflare SMTP token</b>
                <span className="muted">An API token with Email Sending permission. Mail apps then send through
                  smtp.mx.cloudflare.net on port 465, which Outlook can reach. Clear it to send through this server.</span>
              </div>
              <input type="password" autoComplete="off" spellCheck={false} value={token}
                onChange={(e) => setToken(e.target.value)}
                onBlur={() => token.trim() !== s.smtp_token && put({ smtp_token: token.trim() })} />
            </div>
            <Switch title="Save sent copies" checked={s.save_sent_copy} onChange={(v) => put({ save_sent_copy: v })}>
              File a copy of every sent message in Sent. Leave off for Outlook, which does this itself.
            </Switch>
            <div className="switch-row">
              <div className="txt">
                <b>Max message size</b>
                <span className="muted">Largest message accepted, in MB. Cloudflare's limit is 25.</span>
              </div>
              <input type="number" min={1} max={25} className="num-input" value={maxMB}
                onChange={(e) => setMaxMB(e.target.value)}
                onBlur={() => Number(maxMB) !== s.max_message_mb && put({ max_message_mb: Number(maxMB) })} />
            </div>
          </>}
        </div>
      </Panel>
      <Panel title="Connections" icon="server">
        <div className="table-wrap">
          <table>
            <tbody>
              <ConnRow icon="cloud" name="Cloudflare" kind={st.cf}
                detail={cf?.connected ? `${plural(ov?.domains?.length ?? 0, "domain", "domains")} · connected ${fmtTime(ov?.connected_at)}` : "Not connected"}>
                <MoreMenu items={() => cf?.connected ? [
                  { label: "Repair setup", icon: "refresh", onClick: () => open(<RepairCloudflare domains={ov?.domains ?? []} />) },
                  { label: "Sign in again", icon: "key", onClick: () => open(<Wizard step={1} />) },
                ] : [{ label: "Set up", icon: "plus", onClick: () => open(<Wizard step={1} />) }]} />
              </ConnRow>
              <ConnRow icon="globe" name="Tailscale Funnel" kind={st.ts}
                detail={ts?.dns_name ? `${ts.dns_name} · :443 IMAP, :8443 SMTP` : "Not enabled"}>
                <MoreMenu items={() => [
                  { label: ts?.funnel_imap ? "Re-apply Funnel" : "Enable Funnel", icon: "refresh", onClick: () => enableTailscale() },
                ]} />
              </ConnRow>
            </tbody>
          </table>
        </div>
      </Panel>
      <Panel title="Admin access" icon="info">
        <div className="panel-body muted">
          The container publishes this page on <code>{location.host}</code>, on this computer's loopback only, so other
          devices can't reach it.
        </div>
      </Panel>
    </>
  );
}

interface ConnRowProps {
  icon: IconName;
  name: string;
  detail: string;
  kind: Health;
  children: ReactNode;
}

function ConnRow({ icon, name, detail, kind, children }: ConnRowProps) {
  return (
    <tr>
      <td><span className="user-cell"><Icon name={icon} /><b>{name}</b></span></td>
      <td className="wrap muted">{detail}</td>
      <td><Badge kind={kind}>{healthText[kind]}</Badge></td>
      <td className="act">{children}</td>
    </tr>
  );
}
