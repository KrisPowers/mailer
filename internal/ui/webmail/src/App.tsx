import { useCallback, useEffect, useState, type FormEvent, type ReactNode } from "react";
import { message } from "../../shared/format";
import { Icon } from "../../shared/Icon";
import { Callout, Notices, PasswordField, TextField } from "../../shared/ui";
import { api } from "./api";
import { Mail } from "./Mail";
import type { Me } from "./types";

export function App() {
  return (
    <Notices>
      <Root />
    </Notices>
  );
}

// Root shows the sign-in until there's a session, and the mail after.
function Root() {
  const [me, setMe] = useState<Me | null>(null);
  const [state, setState] = useState<"loading" | "out" | "in" | "error">("loading");
  const [problem, setProblem] = useState("");

  const load = useCallback(async () => {
    try {
      const s = await api.session();
      if (!s.signed_in) {
        setMe(null);
        return setState("out");
      }
      setMe(await api.me());
      setState("in");
    } catch (e) {
      setProblem(message(e));
      setState("error");
    }
  }, []);

  useEffect(() => {
    load();
    const out = () => {
      setMe(null);
      setState("out");
    };
    addEventListener("signed-out", out);
    return () => removeEventListener("signed-out", out);
  }, [load]);

  if (state === "loading") return <Frame><p className="setup-note"><span className="spinner small" />Opening your mail</p></Frame>;
  if (state === "error") {
    return (
      <Frame>
        <div className="setup-card card">
          <div className="step-head"><h1>Can't reach the mail server</h1><p className="muted">{problem}</p></div>
          <div className="setup-foot"><button type="button" className="btn primary" onClick={load}><Icon name="refresh" size={18} />Try again</button></div>
        </div>
      </Frame>
    );
  }
  if (state === "out" || !me) return <SignIn onIn={load} />;
  return <Mail me={me} setMe={setMe} onSignOut={async () => {
    await api.logout().catch(() => {});
    setMe(null);
    setState("out");
  }} />;
}

export function Frame({ children }: { children: ReactNode }) {
  return (
    <div className="setup">
      <div className="setup-brand"><span className="brand-mark"><Icon name="mail" size={20} /></span>Mail</div>
      <main className="setup-main">{children}</main>
    </div>
  );
}

function SignIn({ onIn }: { onIn: () => Promise<void> }) {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState(false);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem("");
    try {
      await api.login(email.trim(), password);
      await onIn();
    } catch (err) {
      setProblem(message(err));
      setPassword("");
      setBusy(false);
    }
  };
  return (
    <Frame>
      <form className="setup-card card" onSubmit={submit}>
        <div className="step-head">
          <h1>Sign in</h1>
          <p className="muted">With your email address and its password.</p>
        </div>
        <TextField label="Email address" type="email" autoFocus autoComplete="username" value={email} onChange={setEmail} />
        <PasswordField label="Password" autoComplete="current-password" value={password} onChange={setPassword} />
        {problem && <Callout tone="bad" icon="error">{problem}</Callout>}
        <div className="setup-foot">
          <button className="btn primary" disabled={busy || !email.trim() || !password}>
            {busy && <span className="spinner small on-accent" />}Sign in<Icon name="arrow_forward" size={18} />
          </button>
        </div>
      </form>
      <p className="setup-note"><Icon name="lock" size={16} />Your admin makes accounts and sets their passwords.</p>
    </Frame>
  );
}
