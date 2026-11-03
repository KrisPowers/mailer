import { useCallback, useState } from "react";
import { message } from "../../shared/format";
import { Icon } from "../../shared/Icon";
import { Callout, Card, Copy } from "../../shared/ui";
import { api } from "./api";
import { useOverview, type Health } from "./overview";
import type { ClientSettings, Step, TailscaleResult } from "./types";

// Steps lists what a setup, a repair or a test did, each with how it went.
export function Steps({ steps, error }: { steps?: Step[]; error?: string }) {
  return (
    <>
      {!!steps?.length && (
        <ul className="check-list">
          {steps.map((s, i) => (
            <li key={i} className="check-row">
              <span className={"dot " + (s.ok ? "ok" : "bad")} />
              <span className="check-name">{s.name}</span>
              {s.detail && <span className="check-detail">{s.detail}</span>}
            </li>
          ))}
        </ul>
      )}
      {error && <Callout tone="bad" icon="error">{error}</Callout>}
    </>
  );
}

interface Value {
  label: string;
  value: string;
  // Described rather than a value to copy, such as "the account's password".
  plain?: boolean;
}

// credentialRows lists what a mail app needs, for one address or, without
// one, in general. The account password isn't stored readably, so it's
// described unless a fresh one is passed in.
export function credentialRows(c: ClientSettings, email?: string, password?: string) {
  const user: Value = email ? { label: "Username", value: email } : { label: "Username", value: "The full email address", plain: true };
  const pw: Value = password ? { label: "Password", value: password } : { label: "Password", value: "The account's password", plain: true };
  const imap: Value[] = [
    { label: "Server", value: c.server },
    { label: "Port", value: String(c.imap_port) },
    { label: "Security", value: "SSL/TLS", plain: true },
    user, pw,
  ];
  const smtp: Value[] = [
    { label: "Server", value: c.smtp_server },
    { label: "Port", value: String(c.smtp_port) },
    { label: "Security", value: "SSL/TLS", plain: true },
    ...(c.smtp_user ? [{ label: "Username", value: c.smtp_user }, { label: "Password", value: c.smtp_password ?? "" }] : [user, pw]),
  ];
  return { imap, smtp };
}

export function credentialText(c: ClientSettings, email: string, password?: string) {
  const { imap, smtp } = credentialRows(c, email, password);
  const lines = (rows: Value[]) => rows.map((r) => `  ${r.label}: ${r.value}`);
  return [`Account: ${email}`, "", "Incoming mail (IMAP)", ...lines(imap), "", "Outgoing mail (SMTP)", ...lines(smtp)].join("\n");
}

function Facts({ rows }: { rows: Value[] }) {
  return (
    <dl className="facts-list">
      {rows.map((r) => (
        <div key={r.label} className="fact">
          <dt>{r.label}</dt>
          <dd>{r.plain ? <span>{r.value}</span> : <Copy text={r.value} />}</dd>
        </div>
      ))}
    </dl>
  );
}

// CredentialCards are the incoming and outgoing settings side by side.
export function CredentialCards({ c, email, password }: { c: ClientSettings; email?: string; password?: string }) {
  const { imap, smtp } = credentialRows(c, email, password);
  return (
    <div className="pair">
      <Card title={<><Icon name="inbox" />Incoming mail (IMAP)</>}><Facts rows={imap} /></Card>
      <Card title={<><Icon name="send" />Outgoing mail (SMTP)</>}><Facts rows={smtp} /></Card>
    </div>
  );
}

// useEnableTailscale turns on Funnel and keeps how it went, for the page to
// show: done, or what Tailscale wants first with the link to do it.
export function useEnableTailscale() {
  const { reload } = useOverview();
  const [result, setResult] = useState<TailscaleResult | null>(null);
  const [busy, setBusy] = useState(false);
  const enable = useCallback(async () => {
    setBusy(true);
    setResult(null);
    let r: TailscaleResult;
    try {
      r = await api.enableTailscale();
    } catch (err) {
      r = { error: message(err) };
    }
    setResult(r);
    setBusy(false);
    await reload();
    return r;
  }, [reload]);
  return { enable, result, busy };
}

export function TailscaleOutcome({ result }: { result: TailscaleResult | null }) {
  if (!result) return null;
  if (result.ok) return <Callout tone="ok" icon="check_circle">Funnel is on at <b className="mono">{result.dns_name}</b>.</Callout>;
  return (
    <Callout tone="warn" icon="warning" action={result.action_url ? <a className="btn small" href={result.action_url} target="_blank" rel="noopener noreferrer"><Icon name="open_in_new" size={18} />Open Tailscale</a> : undefined}>
      {result.error || "Funnel couldn't be turned on."}{result.action_url && " Then press Enable again."}
    </Callout>
  );
}

// What each sign-in result means, and what to change.
export const RESULTS: Record<string, { label: string; tone: Health; fix: string }> = {
  ok: { label: "Signed in", tone: "ok", fix: "" },
  bad_password: { label: "Wrong password", tone: "bad", fix: "Re-enter the password in the app, or set a new one." },
  unknown_user: { label: "No such account", tone: "bad", fix: "The username must be the full email address." },
  blocked: { label: "Blocked", tone: "bad", fix: "Too many failures from this address in 15 minutes. Fix the password, then unblock the address." },
  locked: { label: "Refused", tone: "warn", fix: "Sign-in or mail apps are off for this account, under Overview." },
  needs_tls: { label: "Not encrypted", tone: "bad", fix: "Set the app's security to SSL/TLS." },
  unsupported: { label: "Method not offered", tone: "warn", fix: "Choose normal password sign-in in the app." },
  malformed: { label: "Unreadable sign-in", tone: "warn", fix: "Remove the account from the app and add it again." },
  no_sign_in: { label: "Left early", tone: "warn", fix: "The app connected but left before signing in." },
};

export const PROTO: Record<string, string> = { imap: "IMAP", smtp: "SMTP", web: "Webmail" };
