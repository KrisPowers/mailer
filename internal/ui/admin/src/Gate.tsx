import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react";
import { message } from "../../shared/format";
import { Icon } from "../../shared/Icon";
import { Callout, Copy, PasswordField, Row, Switch, TextField } from "../../shared/ui";
import { api } from "./api";
import { currentUser, deviceId, onSessionChange, refresh, setDeviceId, signedIn } from "./session";

type Phase =
  | { kind: "loading" }
  | { kind: "error"; message: string }
  | { kind: "setup" }
  | { kind: "setup-elsewhere" }
  | { kind: "pending"; code: string }
  | { kind: "login" }
  | { kind: "in" };

// Gate stands in front of the dashboard. A browser gets its device ID on
// its first visit; until an admin approves that device it sees only the
// waiting page, and once approved, the sign-in. The very first visit, on the
// computer running mailer, is the setup instead.
export function Gate({ children }: { children: ReactNode }) {
  const [phase, setPhase] = useState<Phase>({ kind: "loading" });

  const check = useCallback(async () => {
    try {
      let st = await api.authStatus();
      if (st.setup && !st.local) return setPhase({ kind: "setup-elsewhere" });
      if (!st.device) {
        // No ID yet, or the server forgot it (removed, or never approved).
        const d = await api.newDevice();
        setDeviceId(d.id);
        st = { ...st, device: { code: d.code, approved: false } };
      }
      const code = st.device!.code;
      if (st.setup) return setPhase({ kind: "setup" });
      if (!st.device!.approved) return setPhase({ kind: "pending", code });
      if (currentUser() || await refresh()) return setPhase({ kind: "in" });
      setPhase({ kind: "login" });
    } catch (err) {
      setPhase({ kind: "error", message: message(err) });
    }
  }, []);

  useEffect(() => { check(); }, [check]);
  // Signing out, or the session ending, comes back here.
  useEffect(() => onSessionChange(() => { if (!currentUser()) check(); }), [check]);

  // A waiting device checks every 10 seconds whether it was approved.
  useEffect(() => {
    if (phase.kind !== "pending") return;
    const t = setInterval(check, 10_000);
    return () => clearInterval(t);
  }, [phase.kind, check]);

  switch (phase.kind) {
    case "in": return <>{children}</>;
    case "loading": return <Frame><p className="setup-note"><span className="spinner small" />Connecting</p></Frame>;
    case "setup": return <FirstRun onDone={() => setPhase({ kind: "in" })} />;
    case "setup-elsewhere": return (
      <Frame>
        <div className="setup-card card">
          <StepHead title="Setup isn't finished" sub="Mailer is set up once, on the computer it runs on." />
          <p>Open <code>http://localhost:8080</code> there to make the admin account. This page works once that's done and an admin has approved this device.</p>
        </div>
      </Frame>
    );
    case "pending": return <Pending code={phase.code} onCheck={check} />;
    case "login": return <SignIn />;
    case "error": return (
      <Frame>
        <div className="setup-card card">
          <StepHead title="Can't reach Mailer" sub={phase.message} />
          <div className="setup-foot"><button type="button" className="btn primary" onClick={check}><Icon name="refresh" size={18} />Try again</button></div>
        </div>
      </Frame>
    );
  }
}

// Frame is the page every screen before the dashboard sits on, under its name.
export function Frame({ children }: { children: ReactNode }) {
  return (
    <div className="setup">
      <div className="setup-brand"><span className="brand-mark"><Icon name="mail" size={20} /></span>Mailer</div>
      <main className="setup-main">{children}</main>
    </div>
  );
}

export function StepHead({ title, sub }: { title: string; sub: ReactNode }) {
  return (
    <div className="step-head">
      <h1>{title}</h1>
      <p className="muted">{sub}</p>
    </div>
  );
}

function Pending({ code, onCheck }: { code: string; onCheck: () => Promise<void> }) {
  const id = deviceId();
  return (
    <Frame>
      <div className="setup-card card">
        <StepHead title="This device needs approval" sub={<>An admin approves it in the dashboard, under <b>Devices</b>. Give them this code to find it.</>} />
        <div className="device-code">{code}</div>
        {id && <p className="device-id">Device ID <Copy text={id} /></p>}
        <div className="setup-foot"><button type="button" className="btn" onClick={onCheck}><Icon name="refresh" size={18} />Check now</button></div>
      </div>
      <p className="setup-note"><Icon name="devices" size={16} />This page checks by itself every few seconds. Clearing this browser's site data gives it a new ID to approve.</p>
    </Frame>
  );
}

function SignIn() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem("");
    try {
      signedIn(await api.login(username.trim(), password));
    } catch (err) {
      setProblem(message(err));
      setPassword("");
      setBusy(false);
    }
  };
  return (
    <Frame>
      <form className="setup-card card" onSubmit={submit}>
        <StepHead title="Sign in" sub="The admin dashboard: accounts, access and traffic." />
        <TextField label="Username" autoFocus autoComplete="username" value={username} onChange={setUsername} />
        <PasswordField label="Password" autoComplete="current-password" value={password} onChange={setPassword} />
        {problem && <Callout tone="bad" icon="error">{problem}</Callout>}
        <div className="setup-foot">
          <button className="btn primary" disabled={busy || !username.trim() || !password}>
            {busy && <span className="spinner small on-accent" />}Sign in<Icon name="arrow_forward" size={18} />
          </button>
        </div>
      </form>
    </Frame>
  );
}

const USERNAME = /^[a-z0-9][a-z0-9._-]{2,31}$/i;
const LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/i;
const MIN_PASSWORD = 12;
const STEPS = ["Admin account", "Tailscale", "Remote access", "Review"];

// FirstRun is the one-time setup, shown only on the computer running mailer
// before any account exists: the admin account and the settings that used to
// be the container's environment. Finishing signs the admin in on this
// device, which it approves.
function FirstRun({ onDone }: { onDone: () => void }) {
  const [step, setStep] = useState(0);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [hostname, setHostname] = useState("mail");
  const [authKey, setAuthKey] = useState("");
  const [remote, setRemote] = useState(false);
  const [subdomain, setSubdomain] = useState("mail-admin");
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);

  const ready = [
    USERNAME.test(username.trim()) && password.length >= MIN_PASSWORD && password === confirm,
    LABEL.test(hostname.trim()),
    LABEL.test(subdomain.trim()),
    true,
  ];
  const last = STEPS.length - 1;

  const finish = async () => {
    setBusy(true);
    setProblem("");
    try {
      signedIn(await api.setup({
        username: username.trim(), password,
        tailscale: { hostname: hostname.trim(), auth_key: authKey.trim() },
        remote: { enabled: remote, subdomain: subdomain.trim() },
      }));
      onDone();
    } catch (err) {
      setProblem(message(err));
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
      <StepHead title="Create the admin account" sub="It signs in to this dashboard, approves devices and makes every other account. There's no sign-up." />
      <TextField label="Username" autoFocus autoComplete="username" value={username} onChange={setUsername}
        hint="3 to 32 letters, digits, dots, dashes or underscores" />
      <PasswordField label="Password" value={password} onChange={setPassword} hint={passwordHint} />
      <PasswordField label="Password again" value={confirm} onChange={setConfirm} />
    </>,
    <>
      <StepHead title="Tailscale" sub="Mail apps reach this server through Tailscale Funnel, by its name on your tailnet." />
      <TextField label="Name on your tailnet" autoFocus value={hostname} onChange={setHostname} mono
        hint={<>Mail apps connect to <code>{hostname.trim() || "name"}.your-tailnet.ts.net</code></>} />
      <PasswordField label="Auth key (optional)" value={authKey} onChange={setAuthKey}
        hint={<>A <a href="https://tailscale.com/kb/1085/auth-keys" target="_blank" rel="noopener noreferrer">Tailscale auth key</a> signs the server in by itself. Without one, a sign-in link shows when you enable Funnel.</>} />
    </>,
    <>
      <StepHead title="Remote access" sub="Serve this dashboard through your Cloudflare Tunnel, to open it from anywhere. Every visitor still needs a device you approved and an account you made." />
      <Row title="Open the dashboard from anywhere" sub="Off keeps it on this computer. It starts once Cloudflare is connected, and can change later in Settings.">
        <Switch label="Open the dashboard from anywhere" on={remote} onChange={setRemote} />
      </Row>
      <TextField label="Address" value={subdomain} onChange={setSubdomain} mono
        hint={<>Served at <code>{subdomain.trim() || "name"}.your-domain</code>, on your primary domain</>} />
    </>,
    <>
      <StepHead title="Ready" sub="Check these, then finish. Next comes connecting Cloudflare, which sets up your domains." />
      <dl className="facts-list flush">
        <dt>Admin</dt><dd>{username.trim().toLowerCase()}</dd>
        <dt>Tailscale name</dt><dd className="mono">{hostname.trim().toLowerCase()}</dd>
        <dt>Auth key</dt><dd>{authKey.trim() ? "Set" : "None, sign in with a link later"}</dd>
        <dt>Remote access</dt><dd>{remote ? <span className="mono">{subdomain.trim().toLowerCase()}.your-domain</span> : "Off"}</dd>
        <dt>This device</dt><dd>Approved</dd>
      </dl>
      {problem && <Callout tone="bad" icon="error">{problem}</Callout>}
    </>,
  ];

  return (
    <Frame>
      <ol className="steps" aria-label="Setup steps">
        {STEPS.map((label, i) => (
          <li key={label} className={"step " + (i === step ? "now" : i < step ? "done" : "later")} aria-current={i === step ? "step" : undefined}>
            <span className="step-mark">{i < step ? <Icon name="check" size={16} /> : i + 1}</span>
            <span className="step-label">{label}</span>
          </li>
        ))}
      </ol>
      <form className="setup-card card" key={step} onSubmit={submit} noValidate>
        {panels[step]}
        <div className="setup-foot">
          {step > 0 && <button type="button" className="btn text" disabled={busy} onClick={() => setStep(step - 1)}>Back</button>}
          <button className="btn primary" disabled={!ready[step] || busy}>
            {busy && <span className="spinner small on-accent" />}{step < last ? "Next" : "Finish setup"}<Icon name="arrow_forward" size={18} />
          </button>
        </div>
      </form>
      <p className="setup-note"><Icon name="lock" size={16} />Runs once, on this computer. Nothing changes until you finish.</p>
    </Frame>
  );
}
