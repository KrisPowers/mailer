import { useEffect, useState } from "react";
import { api } from "../api";
import { fmtTime } from "../format";
import type { Health } from "../overview";
import type { SignInEvent, Step } from "../types";
import { Button, IconButton } from "../ui/Button";
import { Field, PasswordInput, SearchField, TextInput } from "../ui/Field";
import { PopoverButton } from "../ui/Floating";
import { Icon } from "../ui/Icon";
import { Modal, useModal } from "../ui/Modal";
import { Dot, EmptyRow, Metrics, PageHeader, Section, Showing, Steps, Toolbar } from "../ui/Page";
import { Toggle } from "../ui/Toggle";
import { useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

// What each result means, and what to change in the mail app.
export const RESULTS: Record<string, { label: string; health: Health; fix: string }> = {
  ok: { label: "Signed in", health: "ok", fix: "" },
  bad_password: { label: "Wrong password", health: "bad", fix: "Re-enter the account's password in the app, or set a new one on the account's page." },
  unknown_user: { label: "No such account", health: "bad", fix: "The username must be the full email address of an existing account." },
  blocked: { label: "Blocked", health: "bad", fix: "Too many failures from this address in 15 minutes. Fix the password in the app, then unblock the address." },
  needs_tls: { label: "Not encrypted", health: "bad", fix: "Set the app's security to SSL/TLS, not STARTTLS or none." },
  unsupported: { label: "Method not offered", health: "warn", fix: "Choose normal password sign-in in the app. OAuth, NTLM and Kerberos aren't offered." },
  malformed: { label: "Unreadable sign-in", health: "warn", fix: "The app sent credentials the server couldn't read. Remove the account from the app and add it again." },
  no_sign_in: { label: "Left early", health: "warn", fix: "The app connected but left before signing in. The commands show how far it got." },
};

export function SignIns() {
  const { data, error, reload } = useLoad(api.signIns);
  const { open } = useModal();
  const toast = useToast();
  const [q, setQ] = useState("");
  const [failuresOnly, setFailuresOnly] = useState(false);
  const [hideProbes, setHideProbes] = useState(true);

  useEffect(() => {
    const t = setInterval(reload, 5000);
    return () => clearInterval(t);
  }, [reload]);

  const events = data?.events ?? [];
  const query = q.toLowerCase();
  // Web crawlers reach the IMAP port through Funnel and leave without
  // signing in; they're hidden unless asked for.
  const shown = events.filter((e) => (!hideProbes || e.result !== "no_sign_in" || !!e.user || /^(ID|LOGIN|AUTHENTICATE|CAPABILITY|EHLO|HELO|AUTH|STARTTLS)/.test(e.commands?.[0] ?? ""))
    && (!failuresOnly || e.result !== "ok")
    && [e.user, e.ip, e.client, e.proto, RESULTS[e.result]?.label].some((v) => v?.toLowerCase().includes(query)));
  const day = Date.now() - 864e5;
  const recent = events.filter((e) => e.result !== "no_sign_in" && new Date(e.at).getTime() > day);

  return (
    <>
      <PageHeader title="Sign-ins" actions={<Button variant="primary" icon="login" onClick={() => open(<TestSignIn />)}>Test sign-in</Button>} />
      <Metrics items={[
        { label: "Signed in, last 24 hours", value: recent.filter((e) => e.result === "ok").length },
        { label: "Failed, last 24 hours", value: recent.filter((e) => e.result !== "ok").length },
        { label: "Blocked addresses", value: data?.blocked.length ?? 0, tip: "Five failed sign-ins in 15 minutes block an address for 15 minutes" },
      ]} />
      {!!data?.blocked.length && (
        <Section title="Blocked addresses">
          <div className="table-wrap">
            <table className="table">
              <thead><tr><th className="col-dot" /><th>Address</th><th>Blocked until</th><th className="col-actions">Actions</th></tr></thead>
              <tbody>
                {data.blocked.map((b) => (
                  <tr key={b.ip}>
                    <td className="col-dot"><Dot health="bad" label="Blocked" /></td>
                    <td className="mono">{b.ip}</td>
                    <td>{fmtTime(b.until)}</td>
                    <td className="col-actions">
                      <IconButton icon="restart_alt" label="Unblock" tone="accent" onClick={async () => {
                        await api.unblock(b.ip);
                        toast("Unblocked " + b.ip);
                        await reload();
                      }} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Section>
      )}
      <Toolbar>
        <SearchField value={q} onChange={setQ} />
        <Toggle checked={failuresOnly} onChange={setFailuresOnly}>Only show failures</Toggle>
        <Toggle checked={hideProbes} onChange={setHideProbes}>Hide web crawlers</Toggle>
      </Toolbar>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr><th className="col-dot" /><th>Time</th><th>App</th><th>From</th><th>Account</th><th>Result</th><th className="col-actions">Actions</th></tr>
          </thead>
          <tbody>
            {error ? <EmptyRow cols={7} icon="error">{error}</EmptyRow>
              : !data ? <EmptyRow cols={7}>Loading…</EmptyRow>
              : !shown.length ? <EmptyRow cols={7} icon="key">
                {events.length ? "No sign-ins match." : "No mail app has connected yet. If an app can't sign in and nothing shows here, it isn't reaching this server."}
              </EmptyRow>
              : shown.map((e, i) => <Row key={i} e={e} />)}
          </tbody>
        </table>
      </div>
      {data && <Showing count={shown.length} />}
    </>
  );
}

function Row({ e }: { e: SignInEvent }) {
  const r = RESULTS[e.result] ?? { label: e.result, health: "none" as Health, fix: "" };
  return (
    <tr>
      <td className="col-dot"><Dot health={r.health} label={r.label} /></td>
      <td>{fmtTime(e.at)}</td>
      <td>{e.proto === "imap" ? "Reading (IMAP)" : "Sending (SMTP)"}{e.client && <div className="muted small">{e.client}</div>}</td>
      <td className="mono">{e.ip}</td>
      <td>{e.user || <span className="muted">-</span>}</td>
      <td>{r.label}</td>
      <td className="col-actions">
        {(!!e.detail || !!e.commands?.length || !!r.fix) && (
          <PopoverButton label="Details" content={
            <div className="popover-body">
              <h3>{r.label}</h3>
              {e.detail && <p>{e.detail}</p>}
              {r.fix && <p>{r.fix}</p>}
              {!!e.commands?.length && <p className="muted">Commands: <code>{e.commands.join(", ")}</code></p>}
            </div>
          }><Icon name="info" /></PopoverButton>
        )}
      </td>
    </tr>
  );
}

// TestSignIn signs in through the public address the way a mail app does.
function TestSignIn() {
  const { close } = useModal();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [steps, setSteps] = useState<Step[] | null>(null);
  return (
    <Modal title="Test sign-in" actions={<>
      <Button variant="text" onClick={close}>Close</Button>
      <Button variant="primary" disabled={!email || !password} onClick={async () => {
        setSteps(null);
        setSteps((await api.testSignIn(email, password)).steps);
      }}>Test</Button>
    </>}>
      <p className="muted">Checks the password, then signs in to reading and sending through the same public address mail apps use.</p>
      <Field id="ts-email" label="Email address"><TextInput id="ts-email" autoFocus value={email} onChange={setEmail} /></Field>
      <Field id="ts-pw" label="Password"><PasswordInput id="ts-pw" value={password} onChange={setPassword} /></Field>
      {steps && <Steps steps={steps} />}
    </Modal>
  );
}
