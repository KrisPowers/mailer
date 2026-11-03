import { useCallback, useEffect, useState, type FormEvent } from "react";
import { Dropdown } from "../../shared/Dropdown";
import { browserOf, fmtTime, message, timeAgo } from "../../shared/format";
import { Icon } from "../../shared/Icon";
import { Avatar, Callout, Card, Copy, MoreMenu, PageHead, PasswordField, TextField, useNotify, Welcome } from "../../shared/ui";
import { api } from "./api";
import { useOverview } from "./overview";
import { go } from "./route";
import { useUser } from "./session";
import type { DashUser, Device, DeviceList, Role, SessionList } from "./types";

// Devices are the browsers allowed to reach the sign-in. Each gets its own ID
// on its first visit and waits with a short code until an admin approves it.
export function Devices() {
  const [data, setData] = useState<DeviceList | null>(null);
  const [naming, setNaming] = useState("");
  const [name, setName] = useState("");
  const { reload } = useOverview();
  const notify = useNotify();
  const load = useCallback(() => api.devices().then(setData).catch((e) => notify(message(e), "bad")), [notify]);
  useEffect(() => { load(); }, [load]);

  const devices = data?.devices ?? [];
  const waiting = devices.filter((d) => !d.approved).length;
  const act = async (p: Promise<unknown>, done: string) => {
    try {
      await p;
      notify(done);
      setNaming("");
      await Promise.all([load(), reload()]);
    } catch (e) {
      notify(message(e), "bad");
    }
  };
  const startNaming = (d: Device) => {
    setNaming(d.key);
    setName(d.name ?? "");
  };
  const save = (e: FormEvent, d: Device) => {
    e.preventDefault();
    act(d.approved ? api.renameDevice(d.key, name.trim()) : api.approveDevice(d.key, name.trim()), d.approved ? "Device renamed" : "Approved " + d.code);
  };

  return (
    <>
      <PageHead title="Devices" sub={<>
        <span><b>{devices.length - waiting}</b> approved</span>
        {waiting > 0 && <span className="chip warn">{waiting} waiting</span>}
        <span>Approve only a code you can match with the one on that device's screen</span>
      </>} />
      <div className="page-body stack">
        <div className="card">
          {devices.length ? (
            <div className="table-wrap">
              <table className="table">
                <thead><tr><th>Code</th><th className="grow">Name</th><th>Browser</th><th>Address</th><th>Last seen</th><th /></tr></thead>
                <tbody>
                  {devices.map((d) => {
                    const self = d.key === data?.current;
                    return (
                      <tr key={d.key}>
                        <td><span className="log-event"><span className={"dot " + (d.approved ? "ok" : "warn")} /><span className="mono">{d.code}</span></span></td>
                        <td>
                          {naming === d.key ? (
                            <form className="inline-edit" onSubmit={(e) => save(e, d)}>
                              <input className="input" autoFocus maxLength={60} placeholder="Work laptop" aria-label="Name" value={name} onChange={(e) => setName(e.target.value)} />
                              <button type="button" className="icon-btn small" aria-label="Cancel" title="Cancel" onClick={() => setNaming("")}><Icon name="close" size={18} /></button>
                              <button className="btn primary small">{d.approved ? "Save" : "Approve"}</button>
                            </form>
                          ) : (
                            <span className="chips">
                              {d.name || <span className="muted">No name</span>}
                              {self && <span className="chip plain">This device</span>}
                              {!d.approved && <span className="chip warn">Waiting</span>}
                            </span>
                          )}
                        </td>
                        <td title={d.agent}>{browserOf(d.agent)}</td>
                        <td className="mono muted">{d.ip}</td>
                        <td title={"First seen " + fmtTime(d.first_seen)}>{timeAgo(d.last_seen)}</td>
                        <td>
                          <span className="row-actions">
                            {!d.approved && naming !== d.key && <button type="button" className="btn primary small" onClick={() => startNaming(d)}><Icon name="check" size={18} />Approve</button>}
                            <MoreMenu small label="Device actions" actions={[
                              ...(d.approved ? [{ label: "Rename", icon: "edit" as const, onClick: () => startNaming(d) }] : []),
                              { label: d.approved ? "Remove" : "Deny", icon: "delete", danger: true, disabled: self,
                                confirm: d.approved ? "Remove, it signs out" : "Deny, it's forgotten",
                                onClick: () => act(api.removeDevice(d.key), d.approved ? "Device removed" : "Device denied") },
                            ]} />
                          </span>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          ) : <p className="empty">{data ? "No devices yet" : "Loading"}</p>}
        </div>
      </div>
    </>
  );
}

// A dashboard password made a moment ago, shown on People until it's left.
let made: { name: string; password: string } | null = null;

const ROLES = [
  { value: "user" as Role, label: "User", hint: "Manages mail: accounts, traffic and flagged mail" },
  { value: "admin" as Role, label: "Admin", hint: "Also approves devices, manages people and changes the server" },
];

// People are the accounts that sign in to this dashboard, and the sessions
// open now. There's no sign-up: every account is made here.
export function People() {
  const me = useUser();
  const [users, setUsers] = useState<DashUser[] | null>(null);
  const [shown, setShown] = useState(made);
  const [setting, setSetting] = useState("");
  const [password, setPassword] = useState("");
  const notify = useNotify();
  const load = useCallback(() => api.users().then(setUsers).catch((e) => notify(message(e), "bad")), [notify]);
  useEffect(() => {
    load();
    return () => { made = null; };
  }, [load]);

  const act = async (p: Promise<unknown>, done: string) => {
    try {
      await p;
      notify(done);
      await load();
    } catch (e) {
      notify(message(e), "bad");
    }
  };
  const setPw = async (e: FormEvent) => {
    e.preventDefault();
    try {
      const r = await api.setUserPassword(setting, password);
      setShown({ name: r.username, password: r.password });
      setSetting("");
      setPassword("");
    } catch (err) {
      notify(message(err), "bad");
    }
  };

  return (
    <>
      <PageHead title="People" sub="Who signs in to this dashboard" actions={<a className="btn primary" href="#/people/new"><Icon name="add" size={18} />New person</a>} />
      <div className="page-body stack">
        {shown && (
          <Callout tone="warn" icon="key" action={<button type="button" className="btn small" onClick={() => setShown(null)}>Done</button>}>
            Password for <b>{shown.name}</b>: <Copy text={shown.password} />. This is the only time it's shown; they sign in with it on a device you approved.
          </Callout>
        )}
        {setting && (
          <Card title={`New password for ${setting}`} sub="They're signed out everywhere.">
            <form className="card-pad composer-row" onSubmit={setPw}>
              <PasswordField className="grow" label="Password" generate autoFocus value={password} onChange={setPassword} hint="At least 12 characters. Leave it empty and one is made." />
              <button type="button" className="btn text composer-go" onClick={() => setSetting("")}>Cancel</button>
              <button className="btn primary composer-go" disabled={password !== "" && password.length < 12}>Set</button>
            </form>
          </Card>
        )}
        {users && users.length > 0 && (
          <div className="card">
            <ul className="results">
              {users.map((u) => {
                const self = u.name === me?.name;
                return (
                  <li key={u.name}>
                    <div className="result person-line">
                      <Avatar name={u.name} />
                      <span className="person">
                        <span className="scan-target">{u.name}</span>
                        <small>{u.created_by ? `Made by ${u.created_by}` : "Made at setup"}, {fmtTime(u.created)}</small>
                      </span>
                      <span className="chips">
                        <span className={"chip " + (u.role === "admin" ? "plain" : "")}>{u.role === "admin" ? "Admin" : "User"}</span>
                        {u.disabled && <span className="chip bad">Sign-in off</span>}
                        {self && <span className="chip ok">You</span>}
                      </span>
                      <span className="result-meta">Signed in {timeAgo(u.last_login)}</span>
                      {self ? <a className="icon-btn small" href="#/settings/account" aria-label="Your password" title="Your password"><Icon name="settings" size={18} /></a> : (
                        <MoreMenu small label={`Actions for ${u.name}`} actions={[
                          { label: "Set a new password", icon: "lock_reset", onClick: () => { setSetting(u.name); setPassword(""); } },
                          { label: u.role === "admin" ? "Make a user" : "Make an admin", icon: "person",
                            onClick: () => act(api.setUserRole(u.name, u.role === "admin" ? "user" : "admin"), `${u.name} is now ${u.role === "admin" ? "a user" : "an admin"}`) },
                          { label: u.disabled ? "Turn sign-in on" : "Turn sign-in off", icon: "block",
                            onClick: () => act(api.setUserDisabled(u.name, !u.disabled), `Sign-in ${u.disabled ? "on" : "off"} for ${u.name}`) },
                          { label: "Delete", icon: "delete", danger: true, confirm: "Delete, signs them out", onClick: () => act(api.deleteUser(u.name), "Deleted " + u.name) },
                        ]} />
                      )}
                    </div>
                  </li>
                );
              })}
            </ul>
          </div>
        )}
        <Sessions />
      </div>
    </>
  );
}

// NewPerson makes a dashboard account; the password is the one typed or one
// the server makes, shown once on People.
export function NewPerson() {
  const [name, setName] = useState("");
  const [role, setRole] = useState<Role>("user");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const notify = useNotify();
  const ready = /^[a-z0-9][a-z0-9._-]{2,31}$/i.test(name.trim()) && (password === "" || password.length >= 12);
  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      const r = await api.addUser(name.trim(), password, role);
      made = { name: r.user.name, password: r.password };
      go("#/people");
    } catch (err) {
      notify(message(err), "bad");
      setBusy(false);
    }
  };
  return (
    <form onSubmit={submit}>
      <PageHead back="#/people" title="New person" sub="They sign in to this dashboard on a device you approved." />
      <div className="page-body stack">
        <div className="card form-card">
          <div className="form-grid">
            <TextField label="Username" autoFocus autoComplete="off" value={name} onChange={(v) => setName(v.replace(/[^A-Za-z0-9._-]/g, "").slice(0, 32))}
              hint="3 to 32 letters, digits, dots, dashes or underscores" />
            <div className="tf static">
              <div className="tf-box">
                <Dropdown label="Role" value={role} onChange={setRole} options={ROLES} />
                <span className="tf-label">Role</span>
              </div>
              <div className="tf-hint">{ROLES.find((r) => r.value === role)?.hint}</div>
            </div>
            <PasswordField className="span" label="Password" generate value={password} onChange={setPassword}
              hint="At least 12 characters. Leave it empty and one is made for you." />
          </div>
        </div>
        <div className="form-foot">
          <a className="btn text" href="#/people">Cancel</a>
          <button className="btn primary" disabled={busy || !ready}>{busy && <span className="spinner small on-accent" />}Add person</button>
        </div>
      </div>
    </form>
  );
}

// Sessions are the sign-ins open now: everyone's for an admin, otherwise the
// caller's own.
export function Sessions() {
  const me = useUser();
  const [data, setData] = useState<SessionList | null>(null);
  const notify = useNotify();
  const load = useCallback(() => api.sessions().then(setData).catch((e) => notify(message(e), "bad")), [notify]);
  useEffect(() => { load(); }, [load]);
  const admin = me?.role === "admin";
  if (!data) return null;
  if (!data.sessions.length) return <Welcome icon="devices" title="No sessions" />;
  return (
    <Card title="Signed in now" sub="Ending a session signs it out on its device at once.">
      <div className="table-wrap">
        <table className="table">
          <thead><tr>{admin && <th>Person</th>}<th>Device</th><th>Browser</th><th>Address</th><th>Started</th><th className="grow">Last active</th><th /></tr></thead>
          <tbody>
            {data.sessions.map((x) => {
              const self = x.id === data.current;
              return (
                <tr key={x.id}>
                  {admin && <td>{x.user}</td>}
                  <td><span className="mono">{x.device_code || "-"}</span>{x.device_name && <span className="muted"> {x.device_name}</span>}{self && <span className="chip plain"> This session</span>}</td>
                  <td>{browserOf(x.agent)}</td>
                  <td className="mono muted">{x.ip}</td>
                  <td>{fmtTime(x.created)}</td>
                  <td>{timeAgo(x.last_used)}</td>
                  <td>
                    {!self && (
                      <button type="button" className="btn small danger" onClick={() => api.endSession(x.id).then(() => { notify("Session ended"); return load(); }).catch((e) => notify(message(e), "bad"))}>
                        <Icon name="logout" size={18} />End
                      </button>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </Card>
  );
}
