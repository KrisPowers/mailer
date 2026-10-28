import { useEffect, useRef, useState, type FormEvent, type ReactNode } from "react";
import { api } from "../api";
import { signedIn } from "../session";
import { Field, PasswordInput, TextInput } from "../ui/Field";
import { Icon, type IconName } from "../ui/Icon";
import { Toggle } from "../ui/Toggle";
import { Screen } from "./Screen";

const USERNAME = /^[a-z0-9][a-z0-9._-]{2,31}$/i;
const LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/i;
const MIN_PASSWORD = 12;

const STEPS: [string, IconName][] = [
  ["Welcome", "mail"],
  ["Admin", "person"],
  ["Tailscale", "lan"],
  ["Remote access", "language"],
  ["Finish", "check_circle"],
];

// FirstRun is the one-time setup, shown only on the computer running
// mailer before any account exists. It slides through the admin account
// and the settings that used to be the container's environment, then
// signs the admin in on this device, which it approves.
export function FirstRun({ onDone }: { onDone: () => void }) {
  const [step, setStep] = useState(0);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [hostname, setHostname] = useState("mail");
  const [authKey, setAuthKey] = useState("");
  const [remote, setRemote] = useState(false);
  const [subdomain, setSubdomain] = useState("mail-admin");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const slides = useRef<HTMLDivElement>(null);

  const ready = [
    true,
    USERNAME.test(username.trim()) && password.length >= MIN_PASSWORD && password === confirm,
    LABEL.test(hostname.trim()),
    LABEL.test(subdomain.trim()),
    true,
  ];
  const last = STEPS.length - 1;

  // The new slide's first field takes focus once it's in view.
  useEffect(() => {
    const t = setTimeout(() => {
      slides.current?.children[step]?.querySelector<HTMLElement>("input")?.focus({ preventScroll: true });
    }, 260);
    return () => clearTimeout(t);
  }, [step]);

  const finish = async () => {
    setBusy(true);
    setError("");
    try {
      signedIn(await api.setup({
        username: username.trim(), password,
        tailscale: { hostname: hostname.trim(), auth_key: authKey.trim() },
        remote: { enabled: remote, subdomain: subdomain.trim() },
      }));
      onDone();
    } catch (err) {
      setError((err as Error).message);
      setBusy(false);
    }
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (!ready[step] || busy) return;
    if (step < last) setStep(step + 1);
    else finish();
  };

  const passwordHint = !password ? `At least ${MIN_PASSWORD} characters.`
    : password.length < MIN_PASSWORD ? `${MIN_PASSWORD - password.length} more characters.`
    : confirm && confirm !== password ? "The passwords don't match." : "Long enough.";

  const panels: ReactNode[] = [
    <>
      <h2>Set up mailer</h2>
      <p>This runs once, on this computer. It makes the admin account and takes the settings the server needs. Nothing changes until you finish.</p>
      <ul className="first-run-list">
        <li><Icon name="person" /><span><b>Admin account.</b> The dashboard needs a sign-in from now on, and there's no sign-up: admins make every account.</span></li>
        <li><Icon name="devices" /><span><b>Approved devices.</b> Each browser gets its own device ID and can't reach the sign-in until an admin approves it. This one is approved now.</span></li>
        <li><Icon name="lan" /><span><b>Tailscale and remote access.</b> How mail apps reach the server, and whether the dashboard can be opened from anywhere.</span></li>
      </ul>
    </>,
    <>
      <h2>Admin account</h2>
      <p className="muted">Use it to sign in to the dashboard, approve devices and make accounts for others.</p>
      <Field id="fr-user" label="Username" hint="3 to 32 letters, digits, dots, dashes or underscores.">
        <TextInput id="fr-user" autoComplete="username" value={username} onChange={setUsername} />
      </Field>
      <Field id="fr-pw" label="Password" hint={passwordHint}>
        <PasswordInput id="fr-pw" value={password} onChange={setPassword} />
      </Field>
      <Field id="fr-pw2" label="Password again">
        <PasswordInput id="fr-pw2" value={confirm} onChange={setConfirm} />
      </Field>
    </>,
    <>
      <h2>Tailscale</h2>
      <p className="muted">Mail apps reach this server through Tailscale Funnel, by its name on your tailnet.</p>
      <Field id="fr-ts" label="Name on your tailnet" hint={<>Mail apps connect to <code>{hostname.trim() || "name"}.your-tailnet.ts.net</code>.</>}>
        <TextInput id="fr-ts" value={hostname} onChange={setHostname} />
      </Field>
      <Field id="fr-key" label="Auth key (optional)"
        hint={<>A <a href="https://tailscale.com/kb/1085/auth-keys" target="_blank" rel="noopener noreferrer">Tailscale auth key</a> signs the server in by itself. Without one, the dashboard shows a sign-in link when you enable Funnel.</>}>
        <PasswordInput id="fr-key" value={authKey} onChange={setAuthKey} placeholder="tskey-auth-…" />
      </Field>
    </>,
    <>
      <h2>Remote access</h2>
      <p className="muted">Serve the dashboard through your Cloudflare Tunnel so you can open it from anywhere. It starts once Cloudflare is connected,
        and every visitor still needs a device you approved and an account you made.</p>
      <div className="setting">
        <Toggle checked={remote} onChange={setRemote} />
        <div className="setting-text">
          <b>Open the dashboard from anywhere</b>
          <span className="muted">Off keeps it on this computer only. You can change this later in Settings, Server.</span>
        </div>
      </div>
      <Field id="fr-sub" label="Address" hint={<>The dashboard is served at <code>{subdomain.trim() || "name"}.your-domain</code>, on your primary domain.</>}>
        <div className="input address">
          <input id="fr-sub" type="text" spellCheck={false} autoComplete="off" value={subdomain} onChange={(e) => setSubdomain(e.target.value)} />
          <span className="domain">.your-domain</span>
        </div>
      </Field>
    </>,
    <>
      <h2>Ready</h2>
      <p className="muted">Check these, then finish. Next comes connecting Cloudflare, which sets up your domains.</p>
      <dl className="kv first-run-review">
        <dt>Admin</dt><dd>{username.trim().toLowerCase()}</dd><span />
        <dt>Tailscale name</dt><dd className="mono">{hostname.trim().toLowerCase()}</dd><span />
        <dt>Auth key</dt><dd>{authKey.trim() ? "Set" : "None, sign in with a link later"}</dd><span />
        <dt>Remote access</dt><dd>{remote ? <span className="mono">{subdomain.trim().toLowerCase()}.your-domain</span> : "Off"}</dd><span />
        <dt>This device</dt><dd>Approved</dd><span />
      </dl>
      {error && <div className="callout bad"><Icon name="error" /><span>{error}</span></div>}
    </>,
  ];

  return (
    <Screen wide>
      <form onSubmit={submit} noValidate>
        <ol className="stepper-nav first-run-steps">
          {STEPS.map(([name], i) => (
            <li key={name} className={(i === step ? "current" : "") + (i < step ? " done" : "")}>
              <span className="num">{i < step ? <Icon name="check" size={16} /> : i + 1}</span><span className="step-name">{name}</span>
            </li>
          ))}
        </ol>
        <div className="slider">
          <div className="slides" ref={slides} style={{ transform: `translateX(${-step * 100}%)` }}>
            {panels.map((p, i) => (
              <section key={i} className="slide" inert={i !== step} aria-hidden={i !== step}>{p}</section>
            ))}
          </div>
        </div>
        <div className="gate-actions">
          {step > 0 && <button type="button" className="btn text" disabled={busy} onClick={() => setStep(step - 1)}>Back</button>}
          <button type="submit" className="btn primary" disabled={!ready[step] || busy}>
            {busy && <span className="spinner" aria-hidden="true" />}<span>{step === 0 ? "Start" : step < last ? "Next" : "Finish setup"}</span>
          </button>
        </div>
      </form>
    </Screen>
  );
}
