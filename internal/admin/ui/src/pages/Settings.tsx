import { useEffect, useState } from "react";
import { api } from "../api";
import { Repair } from "../flows/Repair";
import { Setup } from "../flows/Setup";
import { useEnableTailscale } from "../flows/tailscale";
import { fmtTime, plural } from "../format";
import { statusOf, useOverview } from "../overview";
import { href } from "../router";
import { useUser } from "../session";
import type { ServerSettings, Settings as SettingsT } from "../types";
import { Button } from "../ui/Button";
import { Field, PasswordInput, Stepper, TextInput } from "../ui/Field";
import { Icon, type IconName } from "../ui/Icon";
import { useModal } from "../ui/Modal";
import { Dot, PageHeader } from "../ui/Page";
import { Toggle } from "../ui/Toggle";
import { useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";
import { SessionsTable } from "./Access";

type Pane = "mail" | "sending" | "connections" | "server" | "security" | "about";

// The last field marks a pane only admins see.
const PANES: [Pane, string, IconName, boolean?][] = [
  ["mail", "Mail handling", "mail"],
  ["sending", "Sending", "send"],
  ["connections", "Connections", "lan"],
  ["server", "Server", "dns", true],
  ["security", "Security", "lock"],
  ["about", "About", "info"],
];

// Settings follows Docker Desktop's settings: sections on the left, the
// chosen section's options on the right. Each change saves at once.
export function Settings() {
  const { data, error } = useLoad(api.settings);
  const [s, setS] = useState<SettingsT | null>(null);
  const [pane, setPane] = useState<Pane>("mail");
  const admin = useUser()?.role === "admin";
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
          {PANES.filter(([, , , adminOnly]) => admin || !adminOnly).map(([key, label, icon]) => (
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
          {pane === "server" && admin && <ServerPane />}
          {pane === "security" && <SecurityPane />}
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

// ServerPane holds what used to be the container's environment: the
// Tailscale name and key, remote access to this dashboard, and the
// debugging switches. Each one saves on its own.
function ServerPane() {
  const { ov, reload } = useOverview();
  const { data, error } = useLoad(api.server);
  const [s, setS] = useState<ServerSettings | null>(null);
  const [host, setHost] = useState("");
  const [key, setKey] = useState("");
  const [sub, setSub] = useState("");
  const [caps, setCaps] = useState("");
  const toast = useToast();

  const load = (v: ServerSettings) => {
    setS(v);
    setHost(v.tailscale_hostname);
    setSub(v.remote_subdomain);
    setCaps(v.imap_caps);
    setKey("");
  };
  useEffect(() => { if (data) load(data); }, [data]);

  // save sends every field; the auth key only when it's being changed.
  const save = async (patch: Partial<ServerSettings>) => {
    if (!s) return;
    const r = await api.saveServer({ ...s, auth_key: undefined, ...patch });
    load(r);
    reload();
    toast(r.warning || "Settings saved", r.warning ? "bad" : "ok");
  };

  if (error) return <div className="callout bad"><Icon name="error" /><span>{error}</span></div>;
  if (!s) return null;
  return (
    <>
      <h2>Server</h2>
      <p className="muted">What used to be set in the container's environment. It's kept in mailer's data folder.</p>
      <div className="settings-list">
        <div className="setting stacked">
          <div className="setting-text">
            <label htmlFor="ts-host"><b>Name on your tailnet</b></label>
            <span className="muted">Mail apps connect to <code>{host.trim() || "name"}.your-tailnet.ts.net</code>, so changing it changes their server name.</span>
          </div>
          <div className="inline-form">
            <TextInput id="ts-host" value={host} onChange={setHost} />
            <Button variant="primary" disabled={host.trim() === s.tailscale_hostname} onClick={() => save({ tailscale_hostname: host.trim() })}>Save</Button>
          </div>
        </div>
        <div className="setting stacked">
          <div className="setting-text">
            <label htmlFor="ts-key"><b>Tailscale auth key</b></label>
            <span className="muted">Signs the server in to your tailnet by itself, without the link shown when enabling Funnel. It's never shown again once saved.</span>
          </div>
          <div className="inline-form">
            <PasswordInput id="ts-key" value={key} onChange={setKey} placeholder={s.auth_key_set ? "Set; type a new one to replace it" : "Not set"} />
            <Button variant="primary" disabled={!key.trim()} onClick={() => save({ auth_key: key.trim() })}>Save</Button>
            {s.auth_key_set && <Button variant="danger" onClick={() => save({ auth_key: "" })}>Clear</Button>}
          </div>
        </div>
        <div className="setting">
          <Toggle checked={s.remote_enabled} onChange={(v) => save({ remote_enabled: v })} />
          <div className="setting-text">
            <b>Open the dashboard from anywhere</b>
            <span className="muted">
              {s.remote_enabled && ov?.dashboard_host
                ? <>Served at <a href={"https://" + ov.dashboard_host} target="_blank" rel="noopener noreferrer">{ov.dashboard_host}</a> through the Cloudflare Tunnel. </>
                : "Serves the dashboard through the Cloudflare Tunnel, on your primary domain. "}
              Visitors still need a device an admin approved and an account an admin made.
            </span>
          </div>
        </div>
        <div className="setting stacked">
          <div className="setting-text">
            <label htmlFor="remote-sub"><b>Dashboard address</b></label>
            <span className="muted">The name under your primary domain that the dashboard is served at.</span>
          </div>
          <div className="inline-form">
            <div className="input address">
              <input id="remote-sub" type="text" spellCheck={false} autoComplete="off" value={sub} onChange={(e) => setSub(e.target.value)} />
              <span className="domain">.{ov?.domain || "your-domain"}</span>
            </div>
            <Button variant="primary" disabled={sub.trim() === s.remote_subdomain} onClick={() => save({ remote_subdomain: sub.trim() })}>Save</Button>
          </div>
        </div>
        <div className="setting">
          <Toggle checked={s.trace} onChange={(v) => save({ trace: v })} />
          <div className="setting-text">
            <b>Log every mail app command</b>
            <span className="muted">For tracking down sign-in and sync problems. Passwords are hidden. Applies to new connections.</span>
          </div>
        </div>
        <div className="setting stacked">
          <div className="setting-text">
            <label htmlFor="imap-caps"><b>IMAP extensions advertised</b></label>
            <span className="muted">Empty uses the default set. Replace it to find an extension a mail app can't handle.</span>
          </div>
          <div className="inline-form">
            <TextInput id="imap-caps" value={caps} onChange={setCaps} placeholder="Default" />
            <Button variant="primary" disabled={caps.trim() === s.imap_caps} onClick={() => save({ imap_caps: caps.trim() })}>Save</Button>
          </div>
        </div>
      </div>
    </>
  );
}

// SecurityPane changes the signed-in account's password. A user also sees
// their sessions here; an admin sees everyone's under Access.
function SecurityPane() {
  const user = useUser();
  const toast = useToast();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  return (
    <>
      <h2>Security</h2>
      <p className="muted">Signed in as <b>{user?.name}</b>, {user?.role === "admin" ? "an admin" : "a user"}.</p>
      <div className="settings-list">
        <div className="setting stacked">
          <div className="setting-text">
            <b>Change password</b>
            <span className="muted">Signs this account out on every other device.</span>
          </div>
          <div className="password-form">
            <Field id="cp-current" label="Current password">
              <PasswordInput id="cp-current" value={current} onChange={setCurrent} autoComplete="current-password" />
            </Field>
            <Field id="cp-new" label="New password" hint="At least 12 characters.">
              <PasswordInput id="cp-new" value={next} onChange={setNext} />
            </Field>
            <Field id="cp-again" label="New password again" hint={again && again !== next ? "The passwords don't match." : undefined}>
              <PasswordInput id="cp-again" value={again} onChange={setAgain} />
            </Field>
            <div>
              <Button variant="primary" disabled={!current || next.length < 12 || next !== again} onClick={async () => {
                await api.changePassword(current, next);
                setCurrent("");
                setNext("");
                setAgain("");
                toast("Password changed");
              }}>Change password</Button>
            </div>
          </div>
        </div>
      </div>
      {user?.role === "admin"
        ? <p className="muted">Every account's sessions, and the devices allowed to sign in, are under <a href={href("access")}>Access</a>.</p>
        : <><h2 className="pane-sub">Your sessions</h2><SessionsTable /></>}
    </>
  );
}

function AboutPane() {
  const { ov } = useOverview();
  return (
    <>
      <h2>About</h2>
      <div className="settings-list">
        <div className="setting">
          <Icon name="lock" />
          <div className="setting-text">
            <b>Dashboard access</b>
            <span className="muted">
              The container publishes this page on this computer's loopback, <code>localhost:8080</code>
              {ov?.dashboard_host && <>, and the Cloudflare Tunnel serves it at <code>{ov.dashboard_host}</code></>}.
              Either way it takes a device an admin approved and a signed-in account.
            </span>
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
