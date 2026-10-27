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

// Cloudflare's token page opens with these permissions already filled in:
// tunnel, Email Worker, Email Routing and Email Sending, and the zone and
// DNS access to publish records, on every account and domain.
const TOKEN_PERMISSIONS = [
  { key: "argotunnel", type: "edit" },
  { key: "workers_scripts", type: "edit" },
  { key: "email_sending", type: "edit" },
  { key: "zone", type: "read" },
  { key: "zone_settings", type: "edit" },
  { key: "dns", type: "edit" },
  { key: "email_routing_rule", type: "edit" },
];

const TOKEN_PAGE = "https://dash.cloudflare.com/profile/api-tokens?" + new URLSearchParams({
  permissionGroupKeys: JSON.stringify(TOKEN_PERMISSIONS),
  accountId: "*",
  zoneId: "all",
  name: "Mailer",
});

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
  if (step === 1) return <CloudflareStep ov={ov} advance={advance} />;
  if (step === 2) return <TailscaleStep ov={ov} advance={advance} />;
  return <AccountStep ov={ov} />;
}

interface FrameProps {
  ov: Overview;
  current: StepNum;
  action: ReactNode;
  children: ReactNode;
}

function Frame({ ov, current, action, children }: FrameProps) {
  return (
    <Modal title="Set up Mailer" wide footer={<><CloseButton />{action}</>}>
      <Steps ov={ov} current={current} />
      {children}
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

interface StepProps {
  ov: Overview;
  advance: () => void;
}

function CloudflareStep({ ov, advance }: StepProps) {
  const toast = useToast();
  const [token, setToken] = useState("");
  const [zones, setZones] = useState<string[] | null>(null);
  const [zone, setZone] = useState("");
  const [status, setStatus] = useState<ReactNode>(null);

  const run = async () => {
    setStatus(<p className="muted">Setting up Cloudflare. This can take a minute.</p>);
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
      setStatus(null);
      return;
    }
    setStatus(<Progress steps={r.steps} error={r.error} />);
    if (!r.error) {
      toast("Cloudflare connected");
      advance();
    }
  };

  return (
    <Frame ov={ov} current={1} action={<AsyncButton className="btn primary" onClick={run}>Connect</AsyncButton>}>
      <p>Cloudflare receives mail for your domain and hands it to this computer. Mailer sets that up for you with a Cloudflare API token.</p>
      <ol className="guide">
        <li>
          <b>Open Cloudflare's token page</b>
          <span className="muted">Sign in if asked. Everything Mailer needs is already filled in.</span>
          <a className="btn sm" href={TOKEN_PAGE} target="_blank" rel="noopener"><Icon name="external" />Open token page</a>
        </li>
        <li>
          <b>Create the token</b>
          <span className="muted">Change nothing. Scroll to the bottom, click <b>Continue to summary</b>, then <b>Create Token</b>.</span>
        </li>
        <li>
          <b>Paste it here</b>
          <span className="muted">Click <b>Copy</b> next to the token. Cloudflare shows it only once.</span>
          <input type="password" placeholder="Paste the token" autoComplete="off" spellCheck={false}
            aria-label="Cloudflare API token" value={token} onChange={(e) => setToken(e.target.value)} />
        </li>
        {zones && (
          <li>
            <b>Choose your domain</b>
            <span className="muted">The token can see several. Add the others later on the Domains page.</span>
            <select aria-label="Domain" value={zone} onChange={(e) => setZone(e.target.value)}>
              {zones.map((z) => <option key={z} value={z}>{z}</option>)}
            </select>
          </li>
        )}
      </ol>
      <p className="muted small">Your domain has to be on Cloudflare already, using Cloudflare's nameservers.</p>
      {status}
    </Frame>
  );
}

function TailscaleStep({ ov, advance }: StepProps) {
  const enable = useEnableTailscale();
  const [r, setR] = useState<TailscaleResult | null>(null);
  const run = async () => {
    const res = await enable(true);
    setR(res);
    if (res.ok) advance();
  };
  return (
    <Frame ov={ov} current={2} action={<AsyncButton className="btn primary" onClick={run}>Enable</AsyncButton>}>
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
    </Frame>
  );
}

function AccountStep({ ov }: { ov: Overview }) {
  const { open } = useModal();
  const { reload } = useOverview();
  const [name, setName] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  return (
    <Frame ov={ov} current={3} action={(
      <AsyncButton className="btn primary" onClick={async () => {
        const c = await api.createAccount(name);
        await reload();
        open(<PasswordModal title="Account created" email={c.email} password={c.password} />);
      }}>Create account</AsyncButton>
    )}>
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
    </Frame>
  );
}
