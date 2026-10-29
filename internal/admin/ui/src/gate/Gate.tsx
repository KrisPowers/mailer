import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react";
import { api } from "../api";
import { currentUser, deviceId, onSessionChange, refresh, setDeviceId, signedIn } from "../session";
import { Button } from "../ui/Button";
import { Field, PasswordInput, TextInput } from "../ui/Field";
import { Icon } from "../ui/Icon";
import { useCopy } from "../ui/Toast";
import { FirstRun } from "./FirstRun";
import { Screen } from "./Screen";

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
// waiting screen, and once approved, the sign-in. The very first visit,
// on the computer running mailer, is the setup instead.
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
      setPhase({ kind: "error", message: (err as Error).message });
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
    case "loading": return <Screen><p className="muted gate-loading"><span className="spinner" /> Connecting…</p></Screen>;
    case "setup": return <FirstRun onDone={() => setPhase({ kind: "in" })} />;
    case "setup-elsewhere": return (
      <Screen icon="lock" title="Setup isn't finished">
        <p>Mailer is set up once, on the computer it runs on. Open <code>http://localhost:8080</code> there to make the admin account.</p>
        <p className="muted">This page can be used once that's done and an admin has approved this device.</p>
      </Screen>
    );
    case "pending": return <Pending code={phase.code} onCheck={check} />;
    case "login": return <SignIn />;
    case "error": return (
      <Screen icon="error" title="Can't reach mailer">
        <p className="muted">{phase.message}</p>
        <div className="gate-actions"><Button variant="primary" icon="refresh" onClick={check}>Try again</Button></div>
      </Screen>
    );
  }
}

// DeviceCode is the short code an admin matches against the device list.
function DeviceCode({ code, full }: { code: string; full?: boolean }) {
  const copy = useCopy();
  const id = deviceId();
  return (
    <div className="device-id">
      <span className="device-code">{code}</span>
      {full && id && (
        <div className="secret">
          <code>{id}</code>
          <button type="button" className="icon-btn" aria-label="Copy device ID" data-tip="Copy device ID" onClick={() => copy(id, "Device ID copied")}>
            <Icon name="content_copy" />
          </button>
        </div>
      )}
    </div>
  );
}

function Pending({ code, onCheck }: { code: string; onCheck: () => Promise<void> }) {
  return (
    <Screen icon="devices" title="This device needs approval">
      <p>Before this browser can sign in, an admin has to approve it in the dashboard, under <b>Access</b>, <b>Devices</b>. Give them this code to find it:</p>
      <DeviceCode code={code} full />
      <p className="muted small">This page checks by itself every few seconds. Clearing this browser's site data gives it a new ID, which needs approving again.</p>
      <div className="gate-actions"><Button icon="refresh" onClick={onCheck}>Check now</Button></div>
    </Screen>
  );
}

function SignIn() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      signedIn(await api.login(username, password));
    } catch (err) {
      setError((err as Error).message);
      setPassword("");
      setBusy(false);
    }
  };
  return (
    <Screen icon="login" title="Sign in">
      <form onSubmit={submit} className="gate-form">
        <Field id="li-user" label="Username">
          <TextInput id="li-user" autoFocus autoComplete="username" value={username} onChange={setUsername} />
        </Field>
        <Field id="li-pw" label="Password">
          <PasswordInput id="li-pw" value={password} onChange={setPassword} autoComplete="current-password" />
        </Field>
        {error && <div className="callout bad"><Icon name="error" /><span>{error}</span></div>}
        <button type="submit" className="btn primary gate-submit" disabled={busy || !username.trim() || !password}>
          {busy && <span className="spinner" aria-hidden="true" />}<span>Sign in</span>
        </button>
      </form>
    </Screen>
  );
}
