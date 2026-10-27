import { useEffect, useState } from "react";
import { api } from "../api";
import { AsyncButton } from "../components/AsyncButton";
import { Badge, type Health } from "../components/Badge";
import { PopoverButton } from "../components/Floating";
import { Icon } from "../components/Icon";
import { EmptyRow, PageHead, Panel, Search } from "../components/Layout";
import { CloseButton, Modal, useModal } from "../components/Modal";
import { useToast } from "../components/Toasts";
import { fmtTime } from "../format";
import { Progress } from "../setup/Progress";
import type { SignInEvent, Step } from "../types";
import { useLoad } from "../useLoad";

// What each result means and what to change in the mail app.
const RESULTS: Record<string, { label: string; kind: Health; fix: string }> = {
  ok: { label: "Signed in", kind: "ok", fix: "" },
  bad_password: { label: "Wrong password", kind: "bad",
    fix: "Re-enter the account's password in the app, or set a new one on the Accounts page." },
  unknown_user: { label: "No such account", kind: "bad",
    fix: "The username must be the full email address of an account on the Accounts page." },
  blocked: { label: "Blocked", kind: "bad",
    fix: "Too many failures from this address in 15 minutes. Fix the password in the app, then unblock the address above." },
  needs_tls: { label: "Not encrypted", kind: "bad", fix: "Set the app's encryption to SSL/TLS, not STARTTLS or None." },
  unsupported: { label: "Method not offered", kind: "warn",
    fix: "Choose normal password authentication in the app. OAuth, NTLM and Kerberos aren't offered." },
  malformed: { label: "Unreadable sign-in", kind: "warn", fix: "The app sent credentials the server couldn't read. Remove and add the account again." },
  no_sign_in: { label: "Left early", kind: "warn",
    fix: "The app connected but gave up before signing in. The commands show how far it got." },
};

export function SignIns() {
  const { data, error, reload } = useLoad(api.signIns);
  const [q, setQ] = useState("");
  const { open } = useModal();
  const toast = useToast();

  useEffect(() => {
    const t = setInterval(reload, 5000);
    return () => clearInterval(t);
  }, [reload]);

  const query = q.toLowerCase();
  const events = (data?.events ?? []).filter((e) => !query
    || [e.user, e.ip, e.client, e.proto, RESULTS[e.result]?.label].some((v) => v?.toLowerCase().includes(query)));

  return (
    <>
      <PageHead title="Sign-ins" sub="Every attempt by Outlook or the Gmail app, newest first">
        <Search value={q} onChange={setQ} placeholder="Filter" />
        <button className="btn primary" onClick={() => open(<TestSignIn />)}><Icon name="key" />Test sign-in</button>
      </PageHead>
      {data && data.blocked.length > 0 && (
        <Panel title="Blocked addresses" icon="alert">
          <div className="table-wrap">
            <table>
              <tbody>
                {data.blocked.map((b) => (
                  <tr key={b.ip}>
                    <td className="mono">{b.ip}</td>
                    <td className="wrap muted">Can't sign in until {fmtTime(b.until)} after too many failures</td>
                    <td className="act">
                      <AsyncButton className="btn sm" onClick={async () => {
                        await api.unblock(b.ip);
                        toast("Unblocked " + b.ip);
                        await reload();
                      }}>Unblock</AsyncButton>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Panel>
      )}
      <Panel>
        <div className="table-wrap">
          <table>
            <thead>
              <tr><th>Time</th><th>App</th><th>From</th><th>Account</th><th>Result</th><th /></tr>
            </thead>
            <tbody>
              {error ? <EmptyRow cols={6}>{error}</EmptyRow>
                : !data ? <EmptyRow cols={6}>Loading…</EmptyRow>
                : !events.length ? <EmptyRow cols={6}>
                  {query ? "No matches." : "No mail app has connected yet. If an app can't sign in and nothing shows "
                    + "here, it isn't reaching this server: check the server name and ports it uses."}
                </EmptyRow>
                : events.map((e, i) => <Row key={i} e={e} />)}
            </tbody>
          </table>
        </div>
      </Panel>
    </>
  );
}

function Row({ e }: { e: SignInEvent }) {
  const r = RESULTS[e.result] ?? { label: e.result, kind: "none" as Health, fix: "" };
  return (
    <tr>
      <td>{fmtTime(e.at)}</td>
      <td className="wrap"><b>{e.proto === "imap" ? "Reading (IMAP)" : "Sending (SMTP)"}</b>
        {e.client && <div className="muted small">{e.client}</div>}</td>
      <td className="mono">{e.ip}</td>
      <td>{e.user}</td>
      <td><Badge kind={r.kind}>{r.label}</Badge></td>
      <td className="act">
        {(!!e.detail || !!e.commands?.length) && (
          <PopoverButton title="Details" content={<>
            <div className="pop-head"><Icon name="info" />{r.label}</div>
            <div className="pop-body small">
              {e.detail && <p>{e.detail}</p>}
              {r.fix && <p>{r.fix}</p>}
              {!!e.commands?.length && <p className="muted">Commands: <span className="mono">{e.commands.join(", ")}</span></p>}
            </div>
          </>}>
            <Icon name="info" />
          </PopoverButton>
        )}
      </td>
    </tr>
  );
}

// TestSignIn signs in through the public address the way a mail app does.
function TestSignIn() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [steps, setSteps] = useState<Step[] | null>(null);
  return (
    <Modal title="Test sign-in" footer={<>
      <CloseButton />
      <AsyncButton className="btn primary" disabled={!email || !password} onClick={async () => {
        setSteps(null);
        setSteps((await api.testSignIn(email, password)).steps);
      }}>Test</AsyncButton>
    </>}>
      <p className="muted">Checks the account and password, then signs in to reading and sending through the same public
        address Outlook and the Gmail app use.</p>
      <div className="field">
        <label htmlFor="t-email">Email address</label>
        <input id="t-email" type="email" autoComplete="off" spellCheck={false} value={email} onChange={(e) => setEmail(e.target.value)} />
      </div>
      <div className="field">
        <label htmlFor="t-pw">Password</label>
        <input id="t-pw" type="password" autoComplete="off" value={password} onChange={(e) => setPassword(e.target.value)} />
      </div>
      {steps && <Progress steps={steps} />}
    </Modal>
  );
}
