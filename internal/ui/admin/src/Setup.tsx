import { useState, type ReactNode } from "react";
import { Dropdown } from "../../shared/Dropdown";
import { message } from "../../shared/format";
import { Icon } from "../../shared/Icon";
import { Callout, PageHead } from "../../shared/ui";
import { api } from "./api";
import { StepHead } from "./Gate";
import { setupStep, useOverview } from "./overview";
import { Steps, TailscaleOutcome, useEnableTailscale } from "./parts";
import type { ConnectResult } from "./types";

const STEPS: [string, string][] = [["cloudflare", "Cloudflare"], ["tailscale", "Tailscale"], ["account", "First account"]];

// Setup walks through the three one-time steps, opening on the first one that
// isn't done, or on the one named (#/setup/cloudflare to sign in again).
export function Setup({ step: named }: { step: string }) {
  const { ov } = useOverview();
  const next = setupStep(ov);
  const forced = STEPS.findIndex(([k]) => k === named);
  const step = forced >= 0 ? forced : next ? next - 1 : -1;
  const done = [!!ov?.cloudflare_setup, !!ov?.tailscale_setup, !!ov?.accounts];

  return (
    <>
      <PageHead title="Setup" sub="Three steps, each saved the moment it's done" />
      <div className="page-body setup-page">
        <ol className="steps" aria-label="Setup steps">
          {STEPS.map(([key, label], i) => (
            <li key={key} className={"step " + (i === step ? "now" : done[i] ? "done" : "later")} aria-current={i === step ? "step" : undefined}>
              <a className="step-link" href={`#/setup/${key}`}>
                <span className="step-mark">{done[i] ? <Icon name="check" size={16} /> : i + 1}</span>
                <span className="step-label">{label}</span>
              </a>
            </li>
          ))}
        </ol>
        <div className="setup-card card" key={step}>
          {!ov ? <p className="muted"><span className="spinner tiny" />Loading</p>
            : step === 0 ? <CloudflareStep />
            : step === 1 ? <TailscaleStep />
            : step === 2 ? <AccountStep />
            : (
              <>
                <StepHead title="Setup is done" sub="Mail arrives through Cloudflare, mail apps reach this server through Tailscale, and the first account exists." />
                <div className="setup-foot"><a className="btn primary" href="#/">Accounts<Icon name="arrow_forward" size={18} /></a></div>
              </>
            )}
        </div>
      </div>
    </>
  );
}

function CloudflareStep() {
  const { ov, reload } = useOverview();
  const [zones, setZones] = useState<string[] | null>(null);
  const [zone, setZone] = useState("");
  const [status, setStatus] = useState<ReactNode>(null);
  const [result, setResult] = useState<ConnectResult | null>(null);
  const [busy, setBusy] = useState(false);

  // The sign-in tab opens before the request, while the click still allows
  // it, and is pointed at Cloudflare once the address arrives.
  const signIn = async (): Promise<ConnectResult> => {
    const tab = window.open("", "_blank");
    const start = await api.signInCloudflare().catch((err) => {
      tab?.close();
      throw err;
    });
    if (!start.url) {
      tab?.close();
      return start;
    }
    if (tab) {
      tab.opener = null;
      tab.location.href = start.url;
    }
    setStatus(<Callout icon="open_in_new">Approve Mailer in the Cloudflare tab (<a href={start.url} target="_blank" rel="noopener noreferrer">open it again</a>). Setup carries on here by itself.</Callout>);
    return api.signInCloudflareWait();
  };

  const run = async () => {
    setBusy(true);
    setResult(null);
    setStatus(zones ? <p className="muted"><span className="spinner tiny" />Setting up {zone}. This can take a minute.</p> : null);
    let r: ConnectResult;
    try {
      r = await (zones ? api.connectCloudflare("", zone) : signIn());
    } catch (err) {
      r = { error: message(err) };
    }
    setBusy(false);
    setStatus(null);
    if (r.choose_zone) {
      setZones(r.choose_zone);
      setZone(r.choose_zone[0] ?? "");
      return;
    }
    setResult(r);
    if (!r.error) await reload();
  };

  return (
    <>
      <StepHead title={ov?.cloudflare_setup ? "Cloudflare is connected" : "Connect Cloudflare"}
        sub="Cloudflare receives mail for your domains and hands it to this server, and sends your replies. Sign in once and Mailer sets up the rest." />
      {zones && (
        <div className="pick">
          <span className="muted">Which domain first? Every other domain on the account is connected by itself afterwards.</span>
          <Dropdown label="Domain" value={zone} onChange={setZone} options={zones.map((z) => ({ value: z, label: z }))} />
        </div>
      )}
      {status}
      {result && <Steps steps={result.steps} error={result.error} />}
      <p className="setup-note">Cloudflare's sign-in page names the app "cf", Cloudflare's own command-line tool, which Mailer signs in through. The domain has to be on Cloudflare already, and sending needs the Workers Paid plan.</p>
      <div className="setup-foot">
        {ov?.cloudflare_setup && <a className="btn text" href="#/setup/tailscale">Next step</a>}
        <button type="button" className="btn primary" disabled={busy} onClick={run}>
          {busy && <span className="spinner small on-accent" />}{zones ? "Use this domain" : ov?.cloudflare_setup ? "Sign in again" : "Connect Cloudflare"}
        </button>
      </div>
    </>
  );
}

function TailscaleStep() {
  const { ov } = useOverview();
  const ts = useEnableTailscale();
  return (
    <>
      <StepHead title="Tailscale Funnel" sub="Lets Outlook and other mail apps reach this server securely, with nothing opened on your router." />
      <ol className="how">
        <li><span className="how-num">1</span><div className="how-body"><b>Press Enable</b><span className="muted">The first time, sign in to Tailscale with the link shown, to add this server to your tailnet.</span></div></li>
        <li><span className="how-num">2</span><div className="how-body"><b>Press Enable again</b><span className="muted">Tailscale asks once to allow Funnel. Approve it and press Enable a last time.</span></div></li>
      </ol>
      <TailscaleOutcome result={ts.result} />
      <div className="setup-foot">
        {ov?.tailscale_setup && <a className="btn text" href="#/setup/account">Next step</a>}
        <button type="button" className="btn primary" disabled={ts.busy} onClick={ts.enable}>{ts.busy && <span className="spinner small on-accent" />}Enable</button>
      </div>
    </>
  );
}

function AccountStep() {
  return (
    <>
      <StepHead title="First account" sub="The mailbox you'll read in the webmail, or in Outlook once its mail apps are on." />
      <div className="setup-foot"><a className="btn primary" href="#/new"><Icon name="add" size={18} />New account</a></div>
    </>
  );
}
