import { useEffect, useState } from "react";
import { api } from "../api";
import { Repair } from "../flows/Repair";
import { Setup } from "../flows/Setup";
import { useEnableTailscale } from "../flows/tailscale";
import { fmtTime, plural } from "../format";
import { statusOf, useOverview } from "../overview";
import type { Settings as SettingsT } from "../types";
import { Button } from "../ui/Button";
import { PasswordInput, Stepper } from "../ui/Field";
import { Icon, type IconName } from "../ui/Icon";
import { useModal } from "../ui/Modal";
import { Dot, PageHeader } from "../ui/Page";
import { Toggle } from "../ui/Toggle";
import { useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

type Pane = "mail" | "sending" | "connections" | "about";

const PANES: [Pane, string, IconName][] = [
  ["mail", "Mail handling", "mail"],
  ["sending", "Sending", "send"],
  ["connections", "Connections", "lan"],
  ["about", "About", "info"],
];

// Settings follows Docker Desktop's settings: sections on the left, the
// chosen section's options on the right. Each change saves at once.
export function Settings() {
  const { data, error } = useLoad(api.settings);
  const [s, setS] = useState<SettingsT | null>(null);
  const [pane, setPane] = useState<Pane>("mail");
  const toast = useToast();

  useEffect(() => { if (data) setS(data); }, [data]);

  // put saves at once and puts the old value back if the server refuses.
  const put = async (patch: Partial<SettingsT>) => {
    if (!s) return;
    const prev = s;
    const next = { ...s, ...patch };
    setS(next);
    try {
      setS(await api.saveSettings(next));
      toast("Settings saved");
    } catch (err) {
      setS(prev);
      toast((err as Error).message, "bad");
    }
  };

  return (
    <>
      <PageHeader title="Settings" />
      <div className="settings">
        <nav className="settings-nav" aria-label="Settings sections">
          {PANES.map(([key, label, icon]) => (
            <button key={key} type="button" className={"nav-item" + (pane === key ? " active" : "")} aria-current={pane === key ? "page" : undefined}
              onClick={() => setPane(key)}>
              <Icon name={icon} /><span className="nav-label">{label}</span>
            </button>
          ))}
        </nav>
        <div className="settings-pane">
          {error && <div className="callout bad"><Icon name="error" /><span>{error}</span></div>}
          {s && pane === "mail" && <MailPane s={s} put={put} />}
          {s && pane === "sending" && <SendingPane s={s} put={put} />}
          {pane === "connections" && <ConnectionsPane />}
          {pane === "about" && <AboutPane />}
        </div>
      </div>
    </>
  );
}

interface PaneProps {
  s: SettingsT;
  put: (patch: Partial<SettingsT>) => Promise<void>;
}

function MailPane({ s, put }: PaneProps) {
  return (
    <>
      <h2>Mail handling</h2>
      <div className="settings-list">
        <div className="setting">
          <Toggle checked={s.reject_dmarc_failures} onChange={(v) => put({ reject_dmarc_failures: v })} />
          <div className="setting-text">
            <b>Reject DMARC failures</b>
            <span className="muted">Refuse mail that fails the sender's DMARC policy when the policy says reject. When off, it goes to Junk.</span>
          </div>
        </div>
        <div className="setting">
          <Toggle checked={s.save_sent_copy} onChange={(v) => put({ save_sent_copy: v })} />
          <div className="setting-text">
            <b>Save sent copies</b>
            <span className="muted">File a copy of every sent message in Sent. Leave off for Outlook, which does this itself.</span>
          </div>
        </div>
        <div className="setting">
          <Stepper id="max-mb" value={s.max_message_mb} min={1} max={25} onChange={(v) => put({ max_message_mb: v })} />
          <div className="setting-text">
            <label htmlFor="max-mb"><b>Largest message, in MB</b></label>
            <span className="muted">Cloudflare accepts up to 25 MB incoming; it sends up to 5 MB.</span>
          </div>
        </div>
      </div>
    </>
  );
}

function SendingPane({ s, put }: PaneProps) {
  const [token, setToken] = useState(s.smtp_token);
  const changed = token.trim() !== s.smtp_token;
  return (
    <>
      <h2>Sending</h2>
      <p className="muted">Outgoing mail always leaves through Cloudflare Email Sending. Mail apps hand it over either to Cloudflare's SMTP relay
        (port 465, which Outlook's servers can reach) or to this server through Tailscale on port 8443.</p>
      <div className="settings-list">
        <div className="setting stacked">
          <div className="setting-text">
            <label htmlFor="smtp-token"><b>Cloudflare SMTP token</b></label>
            <span className="muted">An API token with Email Sending permission. When set, mail apps send through smtp.mx.cloudflare.net with
              username api_token and this token as the password. It's checked with Cloudflare before saving. Clear it to send through this server.</span>
          </div>
          <div className="inline-form">
            <PasswordInput id="smtp-token" value={token} onChange={setToken} placeholder="Not set" />
            <Button variant="primary" disabled={!changed} onClick={() => put({ smtp_token: token.trim() })}>Save</Button>
          </div>
        </div>
      </div>
    </>
  );
}

function ConnectionsPane() {
  const { ov } = useOverview();
  const { open } = useModal();
  const enable = useEnableTailscale();
  const st = statusOf(ov);
  const ts = ov?.checks?.tailscale;
  return (
    <>
      <h2>Connections</h2>
      <div className="settings-list">
        <div className="setting">
          <Dot health={st.cf} />
          <div className="setting-text">
            <b>Cloudflare</b>
            <span className="muted">{ov?.cloudflare_setup
              ? `Receiving for ${plural(ov.domains?.length ?? 0, "domain")} · connected ${fmtTime(ov.connected_at)}`
              : "Not connected"}</span>
          </div>
          <div className="setting-actions">
            {ov?.cloudflare_setup && <Button icon="build" onClick={() => open(<Repair domains={ov.domains ?? []} />)}>Repair</Button>}
            <Button icon="login" onClick={() => open(<Setup step={1} />)}>{ov?.cloudflare_setup ? "Sign in again" : "Connect"}</Button>
          </div>
        </div>
        <div className="setting">
          <Dot health={st.ts} />
          <div className="setting-text">
            <b>Tailscale Funnel</b>
            <span className="muted">{ts?.dns_name ? `${ts.dns_name} · IMAP on 443, SMTP on 8443` : "Not enabled"}</span>
          </div>
          <div className="setting-actions">
            <Button icon="lan" onClick={() => enable()}>{ts?.funnel_imap ? "Re-apply Funnel" : "Enable Funnel"}</Button>
          </div>
        </div>
      </div>
    </>
  );
}

function AboutPane() {
  return (
    <>
      <h2>About</h2>
      <div className="settings-list">
        <div className="setting">
          <Icon name="block" />
          <div className="setting-text">
            <b>Admin access</b>
            <span className="muted">The container publishes this page on <code>{location.host}</code>, on this computer's loopback only, so other devices can't reach it.</span>
          </div>
        </div>
        <div className="setting">
          <Icon name="folder" />
          <div className="setting-text">
            <b>Data</b>
            <span className="muted">Mail, accounts, keys, settings and Tailscale's state live in the container's <code>/data</code>. Back that folder up.</span>
          </div>
        </div>
      </div>
    </>
  );
}
