import { useEffect, useRef, useState, type ReactNode } from "react";
import { api } from "../api";
import { AsyncButton } from "../components/AsyncButton";
import { Icon } from "../components/Icon";
import { CloseButton, Modal, useModal } from "../components/Modal";
import { PasswordField } from "../components/PasswordField";
import { useToast } from "../components/Toasts";
import { setupStep, useOverview } from "../overview";
import type { ConnectResult, Overview, TailscaleResult } from "../types";
import { PasswordModal } from "./PasswordModal";
import { Progress } from "./Progress";
import { useEnableTailscale } from "./tailscale";

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
  const [zones, setZones] = useState<string[] | null>(null);
  const [zone, setZone] = useState("");
  const [status, setStatus] = useState<ReactNode>(null);

  // One click signs in to Cloudflare in the browser and runs the whole
  // setup; only an account with several domains asks which one first.
  const run = async () => {
    setStatus(<p className="muted">{zones ? "Setting up " + zone + ". This can take a minute."
      : "Approve Mailer in the browser window that opened. Setup continues here by itself."}</p>);
    let r: ConnectResult;
    try {
      r = await (zones ? api.connectCloudflare("", zone) : api.signInCloudflare());
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
    <Frame ov={ov} current={1} action={
      <AsyncButton className="btn primary" onClick={run}>{zones ? "Use this domain" : "Connect Cloudflare"}</AsyncButton>
    }>
      <p>Cloudflare receives mail for your domain and hands it to this computer. Connect your Cloudflare account and Mailer sets up the rest.</p>
      {zones && (
        <div className="field">
          <label htmlFor="cf-zone">Which domain first?</label>
          <select id="cf-zone" value={zone} onChange={(e) => setZone(e.target.value)}>
            {zones.map((z) => <option key={z} value={z}>{z}</option>)}
          </select>
          <div className="hint">Add the others later on the Domains page.</div>
        </div>
      )}
      {status}
      <p className="muted small">
        Cloudflare's sign-in page names the app "cf": Cloudflare's own command-line tool, which Mailer signs in through.
        Your domain has to be on Cloudflare already.
      </p>
    </Frame>
  );
}

function TailscaleStep({ ov, advance }: StepProps) {
  const enable = useEnableTailscale();
  const [r, setR] = useState<TailscaleResult | null>(null);
  const [busy, setBusy] = useState(false);
  const run = async () => {
    setR(null);
    setBusy(true);
    const res = await enable(true);
    setBusy(false);
    setR(res);
    if (res.ok) advance();
  };
  return (
    <Frame ov={ov} current={2} action={<AsyncButton className="btn primary" onClick={run}>Enable</AsyncButton>}>
      <div>
        <p>Tailscale Funnel lets Outlook and the Gmail app reach this server securely, with nothing opened on your router.</p>
        <ol className="perm-list">
          <li>Install Tailscale on this machine and sign in <a href="https://tailscale.com/download" target="_blank" rel="noopener">(download)</a>.</li>
          <li>Click Enable. The first time, Tailscale opens in your browser to allow Funnel: approve it there and setup continues by itself.</li>
        </ol>
        {busy && <p className="muted">Enabling Funnel. If Tailscale opened in your browser, approve it there.</p>}
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
  const [password, setPassword] = useState("");
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => input.current?.focus(), []);
  return (
    <Frame ov={ov} current={3} action={(
      <AsyncButton className="btn primary" onClick={async () => {
        const c = await api.createAccount(name, password);
        // Show the password before reloading: once the account exists the
        // wizard counts setup as done and closes itself.
        open(<PasswordModal title="Account created" email={c.email} password={c.password} />);
        await reload();
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
        <PasswordField value={password} onChange={setPassword} />
      </div>
    </Frame>
  );
}
