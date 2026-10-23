import { useEffect, useRef, useState, type ReactNode } from "react";
import { api } from "../api";
import { AsyncButton } from "../components/AsyncButton";
import { Icon } from "../components/Icon";
import { CloseButton, Modal, useModal } from "../components/Modal";
import { useToast } from "../components/Toasts";
import { setupStep, useOverview } from "../overview";
import type { ConnectResult, Overview, TailscaleResult } from "../types";
import { PasswordModal } from "./PasswordModal";
import { Progress } from "./Progress";
import { useEnableTailscale } from "./tailscale";

const PERMISSIONS = [
  "Account · Cloudflare Tunnel · Edit",
  "Account · Workers Scripts · Edit",
  "Zone · Zone · Read",
  "Zone · Zone Settings · Edit",
  "Zone · DNS · Edit",
  "Zone · Email Routing Rules · Edit",
];

type StepNum = 1 | 2 | 3;

// Wizard walks through the three one-time setup steps. It opens on the
// first unfinished step, or on `step` when given.
export function Wizard({ step: forced }: { step?: StepNum }) {
  const { ov, reload } = useOverview();
  const { close } = useModal();
  const toast = useToast();
  const [override, setOverride] = useState(forced);
  const step = override ?? setupStep(ov);

  useEffect(() => {
    if (ov && !step) {
      toast("Setup complete");
      close();
    }
  }, [ov, step, toast, close]);

  // advance moves on once the server reflects the finished step.
  const advance = () => setTimeout(async () => {
    await reload();
    setOverride(undefined);
  }, 900);

  if (!ov || !step) return null;
  const { body, action } =
    step === 1 ? CloudflareStep({ advance }) :
    step === 2 ? TailscaleStep({ advance }) :
    AccountStep({ ov });
  return (
    <Modal title="Set up Mailer" wide footer={<><CloseButton />{action}</>}>
      <Steps ov={ov} current={step} />
      {body}
    </Modal>
  );
}

function Steps({ ov, current }: { ov: Overview; current: StepNum }) {
  const names = ["Cloudflare", "Tailscale", "First account"];
  const done = [ov.cloudflare_setup, ov.tailscale_setup, ov.accounts > 0];
  return (
    <div className="wiz-steps">
      {names.map((n, i) => (
        <div key={n} className={"wiz-step" + (i + 1 === current ? " current" : "") + (done[i] ? " done" : "")}>
          <span className="n">{done[i] ? <Icon name="check" /> : i + 1}</span>
          {n}
        </div>
      ))}
    </div>
  );
}

interface StepView {
  body: ReactNode;
  action: ReactNode;
}

// The step functions are called as hooks from Wizard; each step is always
// rendered in the same position, so hook order is stable per step.
function CloudflareStep({ advance }: { advance: () => void }): StepView {
  const toast = useToast();
  const [token, setToken] = useState("");
  const [zones, setZones] = useState<string[] | null>(null);
  const [zone, setZone] = useState("");
  const [showPerms, setShowPerms] = useState(false);
  const [status, setStatus] = useState<ReactNode>(null);

  const run = async () => {
    setStatus(<p className="muted">Configuring Cloudflare… this can take a minute.</p>);
    let r: ConnectResult;
    try {
      r = await api.connectCloudflare(token.trim(), zones ? zone : "");
    } catch (err) {
      setStatus(null);
      throw err;
    }
    if (r.choose_zone) {
      setZones(r.choose_zone);
      setZone(r.choose_zone[0] ?? "");
      setStatus(<p>This token can see several domains. Choose one and click Connect again.</p>);
      return;
    }
    setStatus(<Progress steps={r.steps} error={r.error} />);
    if (!r.error) {
      toast("Cloudflare connected");
      advance();
    }
  };

  return {
    body: (
      <div>
        <p>Cloudflare receives mail for your domain and passes it to this server through a tunnel. One token sets up everything.</p>
        <div className="field">
          <label htmlFor="cf-token">API token</label>
          <input id="cf-token" type="password" placeholder="Paste Cloudflare API token" autoComplete="off" spellCheck={false}
            value={token} onChange={(e) => setToken(e.target.value)} autoFocus />
          <div className="hint">
            <a href="https://dash.cloudflare.com/profile/api-tokens" target="_blank" rel="noopener">Create a custom token</a>
            {" · "}
            <button className="info-btn" onClick={() => setShowPerms(!showPerms)}><Icon name="info" />Required permissions</button>
            {showPerms && <ul className="perm-list">{PERMISSIONS.map((p) => <li key={p}>{p}</li>)}</ul>}
          </div>
        </div>
        {zones && (
          <div className="field">
            <select value={zone} onChange={(e) => setZone(e.target.value)}>
              {zones.map((z) => <option key={z} value={z}>{z}</option>)}
            </select>
          </div>
        )}
        {status}
      </div>
    ),
    action: <AsyncButton className="btn primary" onClick={run}>Connect</AsyncButton>,
  };
}

function TailscaleStep({ advance }: { advance: () => void }): StepView {
  const enable = useEnableTailscale();
  const [r, setR] = useState<TailscaleResult | null>(null);
  const run = async () => {
    const res = await enable(true);
    setR(res);
    if (res.ok) advance();
  };
  return {
    body: (
      <div>
        <p>Tailscale Funnel lets Outlook and the Gmail app reach this server securely, with nothing opened on your router.</p>
        <ol className="perm-list">
          <li>Install Tailscale on this machine and sign in <a href="https://tailscale.com/download" target="_blank" rel="noopener">(download)</a>.</li>
          <li>Click Enable. The first time, Tailscale may ask you to allow Funnel with one click.</li>
        </ol>
        {r?.ok && <p><Icon name="check" /> Enabled: <code>{r.dns_name}</code></p>}
        {r && !r.ok && (
          <div className="note">
            <Icon name="alert" />
            <div>
              <div>{r.error || "Couldn't enable Funnel."}</div>
              {r.action_url && <>
                <a href={r.action_url} target="_blank" rel="noopener">Open Tailscale <Icon name="external" /></a>
                <div className="muted">After approving, click Enable again.</div>
              </>}
            </div>
          </div>
        )}
      </div>
    ),
    action: <AsyncButton className="btn primary" onClick={run}>Enable</AsyncButton>,
  };
}

function AccountStep({ ov }: { ov: Overview }): StepView {
  const { open } = useModal();
  const { reload } = useOverview();
  const [name, setName] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  return {
    body: (
      <div>
        <p>Create the mailbox you'll use in Outlook or the Gmail app.</p>
        <div className="field">
          <label htmlFor="acct">Address</label>
          <div className="input-suffix">
            <input id="acct" ref={input} type="text" placeholder="you" spellCheck={false} value={name} onChange={(e) => setName(e.target.value)} />
            <span>@{ov.domain}</span>
          </div>
        </div>
      </div>
    ),
    action: (
      <AsyncButton className="btn primary" onClick={async () => {
        const c = await api.createAccount(name);
        await reload();
        open(<PasswordModal title="Account created" email={c.email} password={c.password} />);
      }}>Create account</AsyncButton>
    ),
  };
}
