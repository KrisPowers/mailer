import { useEffect, useState, type FormEvent } from "react";
import { fmtTime, message, plural } from "../../shared/format";
import { Icon, type IconName } from "../../shared/Icon";
import { Appearance } from "../../shared/Appearance";
import { Callout, Card, PasswordField, Row, SaveField, SaveSwitch, useNotify, ValueRow } from "../../shared/ui";
import { api } from "./api";
import { health, useOverview } from "./overview";
import { TailscaleOutcome, useEnableTailscale, webmailHere } from "./parts";
import { Sessions } from "./People";
import { useUser } from "./session";
import { RepairOutcome, useRepair } from "./Status";
import type { ServerSettings, Settings as Saved } from "./types";

// The sections, in the order the sidebar lists them; the last field marks one
// only admins see.
export const SETTINGS: [string, string, IconName, boolean?][] = [
  ["mail", "Mail handling", "mail"],
  ["sending", "Sending", "send"],
  ["connections", "Connections", "cloud"],
  ["server", "Server", "dns", true],
  ["appearance", "Appearance", "brightness_auto"],
  ["account", "Account", "person"],
  ["about", "About", "info"],
];

export function Settings({ section, onSignOut }: { section: string; onSignOut: () => void }) {
  const admin = useUser()?.role === "admin";
  const list = SETTINGS.filter((s) => admin || !s[3]);
  const [key, title] = list.find(([k]) => k === section) ?? list[0];
  const [saved, setSaved] = useState<Saved | null>(null);
  const notify = useNotify();
  useEffect(() => { api.settings().then(setSaved).catch((e) => notify(message(e), "bad")); }, [notify]);

  // put saves at once; the server's answer is what's shown after.
  const put = async (patch: Partial<Saved>) => {
    if (!saved) return;
    setSaved(await api.saveSettings({ ...saved, ...patch }));
  };

  return (
    <>
      <div className="page-head">
        <div className="page-head-main">
          <h1>Settings</h1>
          <div className="page-sub">Saved as you change them</div>
        </div>
      </div>
      <div className="page-body stack flow" key={key}>
        <h2 className="doc-title">{title}</h2>
        {key === "mail" && saved && (
          <Card title="Incoming and sent mail">
            <Row title="Reject DMARC failures" sub="Refuse mail that fails its sender's DMARC policy when that policy says reject. Off files it in Junk.">
              <SaveSwitch label="Reject DMARC failures" on={saved.reject_dmarc_failures} onSave={(v) => put({ reject_dmarc_failures: v })} />
            </Row>
            <Row title="Save sent copies from mail apps" sub="File a copy of what mail apps send in Sent. Leave it off for Outlook, which files its own. The webmail always files one.">
              <SaveSwitch label="Save sent copies" on={saved.save_sent_copy} onSave={(v) => put({ save_sent_copy: v })} />
            </Row>
            <Row title="Largest message, in MB" sub="Cloudflare takes up to 25 MB incoming and sends up to 5 MB.">
              <SaveField label="Largest message, in MB" numeric width={80} value={String(saved.max_message_mb)} onSave={(v) => put({ max_message_mb: Number(v) })} />
            </Row>
          </Card>
        )}
        {key === "sending" && saved && <Sending saved={saved} put={put} />}
        {key === "connections" && <Connections />}
        {key === "server" && admin && <Server />}
        {key === "appearance" && <Appearance />}
        {key === "account" && <AccountSection onSignOut={onSignOut} />}
        {key === "about" && <About />}
      </div>
    </>
  );
}

function Sending({ saved, put }: { saved: Saved; put: (p: Partial<Saved>) => Promise<void> }) {
  const [token, setToken] = useState(saved.smtp_token);
  const [busy, setBusy] = useState(false);
  const notify = useNotify();
  const save = async (value: string) => {
    setBusy(true);
    try {
      await put({ smtp_token: value });
      notify(value ? "Mail apps now send through Cloudflare's relay" : "Mail apps send through this server");
    } catch (e) {
      notify(message(e), "bad");
    } finally {
      setBusy(false);
    }
  };
  return (
    <Card title="Cloudflare SMTP relay" sub="Outgoing mail always leaves through Cloudflare Email Sending. Mail apps hand it over to Cloudflare's relay on port 465, which Outlook's servers reach, or to this server through Tailscale on port 8443.">
      <form className="setting nested" onSubmit={(e: FormEvent) => { e.preventDefault(); save(token.trim()); }}>
        <PasswordField className="grow" label="SMTP token" autoComplete="off" value={token} onChange={setToken}
          hint="An API token with Email Sending permission, checked with Cloudflare before it's saved. Mail apps sign in to smtp.mx.cloudflare.net as api_token with it." />
        {saved.smtp_token && <button type="button" className="btn text" disabled={busy} onClick={() => { setToken(""); save(""); }}>Clear</button>}
        <button className="btn primary" disabled={busy || token.trim() === saved.smtp_token}>{busy && <span className="spinner small on-accent" />}Save</button>
      </form>
    </Card>
  );
}

function Connections() {
  const { ov } = useOverview();
  const ts = useEnableTailscale();
  const fix = useRepair();
  const dot = (h: string) => <span className={"dot " + h} />;
  return (
    <>
      <Card title={<>{dot(health(ov, "cloudflare"))}Cloudflare</>} sub={ov?.cloudflare_setup
        ? `Receiving for ${plural(ov.domains?.length ?? 0, "domain")}, connected ${fmtTime(ov.connected_at)}` : "Not connected"}>
        <Row title="Repair" sub="Checks and re-applies the tunnel, Worker, Email Routing, Email Sending and DNS for every domain.">
          <button type="button" className="btn" disabled={fix.busy || !ov?.cloudflare_setup} onClick={() => fix.repair(ov?.domains ?? [])}><Icon name="build" size={18} />Repair</button>
        </Row>
        <Row title="Sign in again" sub="Connects another Cloudflare account, or renews the sign-in.">
          <a className="btn" href="#/setup/cloudflare"><Icon name="login" size={18} />{ov?.cloudflare_setup ? "Sign in again" : "Connect"}</a>
        </Row>
        <RepairOutcome results={fix.results} busy={fix.busy} />
      </Card>
      <Card title={<>{dot(health(ov, "tailscale"))}Tailscale Funnel</>} sub={ov?.checks?.tailscale.dns_name ? `${ov.checks.tailscale.dns_name}, IMAP on 443 and SMTP on 8443` : "Not enabled"}>
        <Row title="Funnel" sub="Turns Funnel on, or puts it back the way Mailer needs it.">
          <button type="button" className="btn" disabled={ts.busy} onClick={ts.enable}><Icon name="lan" size={18} />{ov?.checks?.tailscale.funnel_imap ? "Re-apply" : "Enable"}</button>
        </Row>
        {ts.result && <div className="card-note"><TailscaleOutcome result={ts.result} /></div>}
      </Card>
    </>
  );
}

// Server holds what used to be the container's environment: the Tailscale
// name and key, remote access to this dashboard, and the debugging switches.
function Server() {
  const { ov, reload } = useOverview();
  const [s, setS] = useState<ServerSettings | null>(null);
  const [key, setKey] = useState("");
  const notify = useNotify();
  useEffect(() => { api.server().then(setS).catch((e) => notify(message(e), "bad")); }, [notify]);

  // save sends every field; the auth key only when it's being changed.
  const save = async (patch: Partial<ServerSettings>) => {
    if (!s) return;
    const r = await api.saveServer({ ...s, auth_key: undefined, ...patch });
    setS(r);
    reload();
    if (r.warning) notify(r.warning, "bad");
  };
  if (!s) return null;
  return (
    <>
      <Card title="Tailscale" sub="How mail apps find this server on your tailnet.">
        <ValueRow title="Name on your tailnet" mono value={s.tailscale_hostname} onSave={(v) => save({ tailscale_hostname: v })}
          hint={<>Mail apps connect to <code>name.your-tailnet.ts.net</code>, so changing it changes their server name</>} />
        <form className="setting nested" onSubmit={(e) => { e.preventDefault(); save({ auth_key: key.trim() }).then(() => setKey("")).catch((err) => notify(message(err), "bad")); }}>
          <PasswordField className="grow" label={s.auth_key_set ? "Auth key, set. Type a new one to replace it" : "Auth key"} autoComplete="off" value={key} onChange={setKey}
            hint="Signs the server in to your tailnet by itself. It's never shown again once saved." />
          {s.auth_key_set && <button type="button" className="btn text" onClick={() => save({ auth_key: "" }).catch((err) => notify(message(err), "bad"))}>Clear</button>}
          <button className="btn primary" disabled={!key.trim()}>Save</button>
        </form>
      </Card>
      <Card title="Remote access" sub="Visitors still need a device an admin approved and an account an admin made.">
        <Row title="Open the dashboard from anywhere" sub={s.remote_enabled && ov?.dashboard_host
          ? <>Served at <a href={"https://" + ov.dashboard_host} target="_blank" rel="noopener noreferrer">{ov.dashboard_host}</a> through the Cloudflare Tunnel</>
          : "Serves it through the Cloudflare Tunnel, on your primary domain"}>
          <SaveSwitch label="Open the dashboard from anywhere" on={s.remote_enabled} onSave={(v) => save({ remote_enabled: v })} />
        </Row>
        <ValueRow title="Dashboard address" mono value={s.remote_subdomain} onSave={(v) => save({ remote_subdomain: v })}
          hint={<>The name under <code>{ov?.domain || "your-domain"}</code> it's served at</>} />
      </Card>
      <Card title="Webmail" sub="Where accounts read and write their mail in a browser. Each visit signs in with the account's address and password.">
        <Row title="Open the webmail from anywhere" sub={s.webmail_enabled && ov?.webmail_host
          ? <>Served at <a href={"https://" + ov.webmail_host} target="_blank" rel="noopener noreferrer">{ov.webmail_host}</a> through the Cloudflare Tunnel</>
          : <>On this computer at <a href={webmailHere()} target="_blank" rel="noopener noreferrer">{webmailHere()}</a>. On, it's served through the Cloudflare Tunnel too</>}>
          <SaveSwitch label="Open the webmail from anywhere" on={s.webmail_enabled} onSave={(v) => save({ webmail_enabled: v })} />
        </Row>
        <ValueRow title="Webmail address" mono value={s.webmail_subdomain} onSave={(v) => save({ webmail_subdomain: v })}
          hint={<>The name under <code>{ov?.domain || "your-domain"}</code> it's served at</>} />
      </Card>
      <Card title="Debugging" sub="For tracking down a mail app that won't sign in or sync. Each applies to new connections.">
        <Row title="Log every mail app command" sub="Passwords are hidden.">
          <SaveSwitch label="Log every mail app command" on={s.trace} onSave={(v) => save({ trace: v })} />
        </Row>
        <ValueRow title="IMAP extensions advertised" mono empty="The default set" value={s.imap_caps} onSave={(v) => save({ imap_caps: v })}
          hint="Replace the set to find an extension a mail app can't handle" />
      </Card>
    </>
  );
}

function AccountSection({ onSignOut }: { onSignOut: () => void }) {
  const user = useUser();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const [busy, setBusy] = useState(false);
  const notify = useNotify();
  const change = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await api.changePassword(current, next);
      setCurrent("");
      setNext("");
      setAgain("");
      notify("Password changed. Every other device was signed out.");
    } catch (err) {
      notify(message(err), "bad");
    } finally {
      setBusy(false);
    }
  };
  return (
    <>
      <Card title="Signed in" sub={<>As <b>{user?.name}</b>, {user?.role === "admin" ? "an admin" : "a user"}.</>}>
        <Row title="Sign out" sub="Ends this browser's session. Mail keeps flowing.">
          <button type="button" className="btn" onClick={onSignOut}><Icon name="logout" size={18} />Sign out</button>
        </Row>
      </Card>
      <Card title="Change password" sub="Signs this account out on every other device.">
        <form className="card-pad" onSubmit={change}>
          <PasswordField label="Current password" autoComplete="current-password" value={current} onChange={setCurrent} />
          <PasswordField label="New password" value={next} onChange={setNext} hint="At least 12 characters." />
          <PasswordField label="New password again" value={again} onChange={setAgain} hint={again && again !== next ? "The passwords don't match." : undefined} />
          <div className="edit-actions">
            <button className="btn primary" disabled={busy || !current || next.length < 12 || next !== again}>Change password</button>
          </div>
        </form>
      </Card>
      {user?.role !== "admin" ? <Sessions /> : <Callout icon="shield">Everyone's sessions, and the devices allowed to sign in, are under <a href="#/people">People</a> and <a href="#/devices">Devices</a>.</Callout>}
    </>
  );
}

function About() {
  const { ov } = useOverview();
  return (
    <Card title="Mailer" sub="A mail server for your own domains, with Cloudflare in front and nothing opened on your router.">
      <Row title="This dashboard" sub="Published on this computer's loopback, and through the tunnel when remote access is on. It takes a device an admin approved and a signed-in account.">
        <span className="mono">{ov?.dashboard_host || location.host}</span>
      </Row>
      <Row title="Data" sub="Mail, accounts, keys, settings and Tailscale's state live in the container's /data. Back that folder up, and keep it private." />
      <Row title="Source"><a className="btn" href="https://github.com/KrisPowers/mailer" target="_blank" rel="noreferrer"><Icon name="open_in_new" size={18} />GitHub</a></Row>
    </Card>
  );
}
