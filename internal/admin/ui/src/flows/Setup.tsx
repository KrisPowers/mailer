import { useEffect, useState, type ReactNode } from "react";
import { api } from "../api";
import { setupStep, useOverview } from "../overview";
import type { ConnectResult, Overview, TailscaleResult } from "../types";
import { Button } from "../ui/Button";
import { Field, PasswordInput, TextInput } from "../ui/Field";
import { Icon } from "../ui/Icon";
import { Modal, useModal } from "../ui/Modal";
import { Steps } from "../ui/Page";
import { Select } from "../ui/Select";
import { useToast } from "../ui/Toast";
import { PasswordShown } from "./Accounts";
import { useEnableTailscale } from "./tailscale";

type StepNum = 1 | 2 | 3;

// Setup walks through the three one-time steps, opening on the first one
// that isn't done (or on step, when given).
export function Setup({ step: forced }: { step?: StepNum }) {
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
  }, 800);

  if (!ov || !step) return null;
  if (step === 1) return <CloudflareStep ov={ov} advance={advance} />;
  if (step === 2) return <TailscaleStep ov={ov} advance={advance} />;
  return <AccountStep ov={ov} />;
}

function Frame({ ov, current, action, children }: { ov: Overview; current: StepNum; action: ReactNode; children: ReactNode }) {
  const { close } = useModal();
  const done = [ov.cloudflare_setup, ov.tailscale_setup, ov.accounts > 0];
  return (
    <Modal title="Set up mailer" wide actions={<><Button variant="text" onClick={close}>Close</Button>{action}</>}>
      <ol className="stepper-nav">
        {["Cloudflare", "Tailscale", "First account"].map((n, i) => (
          <li key={n} className={(i + 1 === current ? "current" : "") + (done[i] ? " done" : "")}>
            <span className="num">{done[i] ? <Icon name="check" size={16} /> : i + 1}</span>{n}
          </li>
        ))}
      </ol>
      {children}
    </Modal>
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

  // The sign-in tab opens before the request, while the click still allows
  // it, and is pointed at Cloudflare once the address arrives.
  const signIn = async (): Promise<ConnectResult> => {
    const tab = window.open("", "_blank");
    const start = await api.signInCloudflare().catch((err) => { tab?.close(); throw err; });
    if (!start.url) {
      tab?.close();
      return start;
    }
    if (tab) {
      tab.opener = null;
      tab.location.href = start.url;
    }
    setStatus(<p className="muted"><span className="spinner" /> Approve mailer in the Cloudflare tab
      (<a href={start.url} target="_blank" rel="noopener noreferrer">open it again</a>). Setup continues here by itself.</p>);
    return api.signInCloudflareWait();
  };

  const run = async () => {
    setStatus(zones ? <p className="muted"><span className="spinner" /> Setting up {zone}. This can take a minute.</p> : null);
    let r: ConnectResult;
    try {
      r = await (zones ? api.connectCloudflare("", zone) : signIn());
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
    setStatus(<Steps steps={r.steps} error={r.error} />);
    if (!r.error) {
      toast("Cloudflare connected");
      advance();
    }
  };

  return (
    <Frame ov={ov} current={1} action={<Button variant="primary" onClick={run}>{zones ? "Use this domain" : "Connect Cloudflare"}</Button>}>
      <p>Cloudflare receives mail for your domain and hands it to this server, and sends your replies. Connect your Cloudflare account and mailer sets up the rest.</p>
      {zones && (
        <Field id="cf-zone" label="Which domain first?" hint="Add the others later on the Domains page.">
          <Select id="cf-zone" value={zone} options={zones} onChange={setZone} />
        </Field>
      )}
      {status}
      <p className="muted small">Cloudflare's sign-in page names the app "cf", Cloudflare's own command-line tool, which mailer signs in through. The domain has to be on Cloudflare already, and sending needs the Workers Paid plan.</p>
    </Frame>
  );
}

function TailscaleStep({ ov, advance }: StepProps) {
  const enable = useEnableTailscale();
  const [r, setR] = useState<TailscaleResult | null>(null);
  return (
    <Frame ov={ov} current={2} action={<Button variant="primary" onClick={async () => {
      setR(null);
      const res = await enable(true);
      setR(res);
      if (res.ok) advance();
    }}>Enable</Button>}>
      <p>Tailscale Funnel lets Outlook and the Gmail app reach this server securely, with nothing opened on your router.</p>
      <ol className="plain-list">
        <li>Click Enable. The first time, sign in to Tailscale with the link shown to add this server to your tailnet.</li>
        <li>Click Enable again. Tailscale asks once to allow Funnel: approve it and click Enable a last time.</li>
      </ol>
      {r?.ok && <div className="callout ok"><Icon name="check_circle" /><span>Enabled on <code>{r.dns_name}</code></span></div>}
      {r && !r.ok && (
        <div className="callout warn">
          <Icon name="warning" />
          <span>{r.error || "Couldn't enable Funnel."}{r.action_url && <> <a href={r.action_url} target="_blank" rel="noopener noreferrer">Open Tailscale</a>, then click Enable again.</>}</span>
        </div>
      )}
    </Frame>
  );
}

function AccountStep({ ov }: { ov: Overview }) {
  const { open } = useModal();
  const { reload } = useOverview();
  const [name, setName] = useState("");
  const [domain, setDomain] = useState(ov.domain);
  const [password, setPassword] = useState("");
  return (
    <Frame ov={ov} current={3} action={<Button variant="primary" disabled={!name.trim()} onClick={async () => {
      const c = await api.createAccount(name.includes("@") ? name : name + "@" + domain, password);
      // Shown before reloading: once the account exists, setup counts as
      // done and closes itself.
      open(<PasswordShown title="Account created" email={c.email} password={c.password} />);
      await reload();
    }}>Create account</Button>}>
      <p>Create the mailbox you'll use in Outlook or the Gmail app.</p>
      <Field id="su-name" label="Address">
        <TextInput id="su-name" autoFocus placeholder="you" value={name} onChange={setName} suffix={"@" + domain} />
      </Field>
      {(ov.domains?.length ?? 0) > 1 && (
        <Field id="su-domain" label="Domain"><Select id="su-domain" value={domain} options={ov.domains ?? []} onChange={setDomain} /></Field>
      )}
      <Field id="su-pw" label="Password" hint="At least 10 characters. Leave empty to generate one.">
        <PasswordInput id="su-pw" value={password} onChange={setPassword} />
      </Field>
    </Frame>
  );
}
