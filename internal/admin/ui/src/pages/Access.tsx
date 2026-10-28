import { useState } from "react";
import { api } from "../api";
import { browserOf, fmtTime, timeAgo } from "../format";
import { useOverview } from "../overview";
import { useUser } from "../session";
import type { DashUser, Device, Role } from "../types";
import { Button, IconButton } from "../ui/Button";
import { Field, PasswordInput, TextInput } from "../ui/Field";
import { Menu, type MenuItem } from "../ui/Floating";
import { Icon } from "../ui/Icon";
import { Confirm, Modal, useModal } from "../ui/Modal";
import { Chip, Dot, EmptyRow, PageHeader, Tabs, Toolbar } from "../ui/Page";
import { Select } from "../ui/Select";
import { useCopy, useToast } from "../ui/Toast";
import { useLoad } from "../useLoad";

type Tab = "devices" | "accounts" | "sessions";

// Access is who can reach the dashboard: the devices allowed to see the
// sign-in, the accounts that sign in, and the sessions open now. Admins
// only.
export function Access() {
  const user = useUser();
  const [tab, setTab] = useState<Tab>("devices");
  if (user?.role !== "admin") {
    return <><PageHeader title="Access" /><p className="muted">Only an admin can manage devices and accounts.</p></>;
  }
  return (
    <>
      <PageHeader title="Access" />
      <Tabs tabs={[["devices", "Devices"], ["accounts", "Dashboard accounts"], ["sessions", "Sessions"]]} value={tab} onChange={setTab} />
      {tab === "devices" && <Devices />}
      {tab === "accounts" && <Users />}
      {tab === "sessions" && <SessionsTable />}
    </>
  );
}

function Devices() {
  const { data, error, reload } = useLoad(api.devices);
  const { reload: reloadOverview } = useOverview();
  const { open } = useModal();
  const toast = useToast();
  const devices = data?.devices ?? [];

  const done = async (msg: string) => {
    toast(msg);
    await Promise.all([reload(), reloadOverview()]);
  };
  const approve = (d: Device) => open(
    <NameDevice title={"Approve " + d.code} label="Approve" device={d}
      onSave={async (name) => { await api.approveDevice(d.key, name); await done("Approved " + d.code); }} />);
  const rename = (d: Device) => open(
    <NameDevice title={"Rename " + d.code} label="Save" device={d}
      onSave={async (name) => { await api.renameDevice(d.key, name); await done("Device renamed"); }} />);
  const remove = (d: Device) => open(
    <Confirm title={d.approved ? "Remove device" : "Deny device"} label={d.approved ? "Remove" : "Deny"}
      message={d.approved
        ? <>Remove <b className="mono">{d.code}</b>{d.name && <> ({d.name})</>}? Anyone signed in on it is signed out, and it has to be approved again to reach the sign-in.</>
        : <>Deny <b className="mono">{d.code}</b>? It's forgotten; if it visits again it gets a new code to approve.</>}
      onYes={async () => { await api.removeDevice(d.key); await done(d.approved ? "Device removed" : "Device denied"); }} />);

  return (
    <>
      <p className="muted access-intro">Each browser that opens the dashboard gets its own device ID, and can't reach the sign-in until it's approved here.
        Approve only a code you can match with the one on the device's screen.</p>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th className="col-dot" /><th>Code</th><th>Name</th><th>Browser</th><th>Address</th><th className="grow">Last seen</th>
              <th className="col-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            {error ? <EmptyRow cols={7} icon="error">{error}</EmptyRow>
              : !data ? <EmptyRow cols={7}>Loading…</EmptyRow>
              : !devices.length ? <EmptyRow cols={7} icon="devices">No devices yet.</EmptyRow>
              : devices.map((d) => {
                const current = d.key === data.current;
                return (
                  <tr key={d.key}>
                    <td className="col-dot"><Dot health={d.approved ? "ok" : "warn"} label={d.approved ? "Approved" : "Waiting for approval"} /></td>
                    <td><span className="mono">{d.code}</span>{current && <Chip health="plain">This device</Chip>}{!d.approved && <Chip health="warn">Waiting</Chip>}</td>
                    <td>{d.name || <span className="muted">-</span>}</td>
                    <td>{browserOf(d.agent)}</td>
                    <td className="mono">{d.ip}</td>
                    <td><span data-tip={"First seen " + fmtTime(d.first_seen)}>{timeAgo(d.last_seen)}</span></td>
                    <td className="col-actions">
                      <span className="row-actions">
                        {d.approved
                          ? <IconButton icon="edit" label="Rename" onClick={() => rename(d)} />
                          : <Button small variant="primary" icon="check" onClick={() => approve(d)}>Approve</Button>}
                        <span className="divider" />
                        <IconButton icon="delete" tone="danger" label={current ? "This is the device you're using" : d.approved ? "Remove device" : "Deny device"}
                          disabled={current} onClick={() => remove(d)} />
                      </span>
                    </td>
                  </tr>
                );
              })}
          </tbody>
        </table>
      </div>
    </>
  );
}

function NameDevice({ title, label, device, onSave }: { title: string; label: string; device: Device; onSave: (name: string) => Promise<void> }) {
  const { close } = useModal();
  const [name, setName] = useState(device.name ?? "");
  return (
    <Modal title={title} actions={<>
      <Button variant="text" onClick={close}>Cancel</Button>
      <Button variant="primary" onClick={async () => { await onSave(name.trim()); close(); }}>{label}</Button>
    </>}>
      {!device.approved && (
        <div className="callout warn"><Icon name="warning" />
          <span>{browserOf(device.agent)} from <span className="mono">{device.ip}</span>, first seen {fmtTime(device.first_seen)}.
            Approve it only if <b className="mono">{device.code}</b> is the code on that device's screen.</span>
        </div>
      )}
      <Field id="dev-name" label="Name" hint="Tells your devices apart, like Work laptop.">
        <TextInput id="dev-name" autoFocus maxLength={60} value={name} onChange={setName} />
      </Field>
    </Modal>
  );
}

const ROLES: Record<string, Role> = { Admin: "admin", User: "user" };
const roleLabel = (r: Role) => (r === "admin" ? "Admin" : "User");

function Users() {
  const me = useUser();
  const { data, error, reload } = useLoad(api.users);
  const { open } = useModal();
  const toast = useToast();
  const users = data ?? [];

  const act = (p: Promise<unknown>, msg: string) =>
    p.then(() => { toast(msg); return reload(); }).catch((err: Error) => toast(err.message, "bad"));
  const menu = (u: DashUser) => (): MenuItem[] => [
    { label: "Set a new password", icon: "lock_reset", onClick: () => open(<SetUserPassword name={u.name} />) },
    { label: u.role === "admin" ? "Make a user" : "Make an admin", icon: "person",
      onClick: () => act(api.setUserRole(u.name, u.role === "admin" ? "user" : "admin"), `${u.name} is now ${u.role === "admin" ? "a user" : "an admin"}`) },
    { label: u.disabled ? "Turn sign-in on" : "Turn sign-in off", icon: "block",
      onClick: () => act(api.setUserDisabled(u.name, !u.disabled), `Sign-in ${u.disabled ? "on" : "off"} for ${u.name}`) },
  ];
  const remove = (u: DashUser) => open(
    <Confirm title="Delete dashboard account" label="Delete"
      message={<>Delete <b>{u.name}</b>? They're signed out everywhere and can't sign in again. Mail accounts aren't touched.</>}
      onYes={async () => { await api.deleteUser(u.name); toast("Deleted " + u.name); await reload(); }} />);

  return (
    <>
      <Toolbar actions={<Button variant="primary" icon="add" onClick={() => open(<NewUser onCreated={reload} />)}>New account</Button>}>
        <p className="muted">Accounts that sign in to this dashboard. There's no sign-up: every account is made here.</p>
      </Toolbar>
      <div className="table-wrap">
        <table className="table">
          <thead>
            <tr>
              <th className="col-dot" /><th>Username</th><th>Role</th><th>Created</th><th className="grow">Last sign-in</th>
              <th className="col-actions">Actions</th>
            </tr>
          </thead>
          <tbody>
            {error ? <EmptyRow cols={6} icon="error">{error}</EmptyRow>
              : !data ? <EmptyRow cols={6}>Loading…</EmptyRow>
              : users.map((u) => {
                const self = u.name === me?.name;
                return (
                  <tr key={u.name}>
                    <td className="col-dot"><Dot health={u.disabled ? "bad" : "ok"} label={u.disabled ? "Sign-in off" : "Active"} /></td>
                    <td>{u.name}{self && <Chip health="plain">You</Chip>}</td>
                    <td><Chip health={u.role === "admin" ? "plain" : "none"}>{roleLabel(u.role)}</Chip></td>
                    <td><span data-tip={u.created_by ? "By " + u.created_by : "At setup"}>{fmtTime(u.created)}</span></td>
                    <td>{timeAgo(u.last_login)}</td>
                    <td className="col-actions">
                      <span className="row-actions">
                        {self ? <a className="icon-btn" href="#settings" aria-label="Change your password in Settings" data-tip="Change your password in Settings"><Icon name="settings" /></a>
                          : <Menu items={menu(u)} />}
                        <span className="divider" />
                        <IconButton icon="delete" tone="danger" label={self ? "You can't delete your own account" : "Delete account"} disabled={self}
                          onClick={() => remove(u)} />
                      </span>
                    </td>
                  </tr>
                );
              })}
          </tbody>
        </table>
      </div>
    </>
  );
}

function NewUser({ onCreated }: { onCreated: () => Promise<void> }) {
  const { open, close } = useModal();
  const [name, setName] = useState("");
  const [role, setRole] = useState("User");
  const [password, setPassword] = useState("");
  return (
    <Modal title="New dashboard account" actions={<>
      <Button variant="text" onClick={close}>Cancel</Button>
      <Button variant="primary" disabled={!name.trim()} onClick={async () => {
        const r = await api.addUser(name, password, ROLES[role]);
        await onCreated();
        open(<UserPassword title="Account created" name={r.user.name} password={r.password} />);
      }}>Create</Button>
    </>}>
      <Field id="nu-name" label="Username" hint="3 to 32 letters, digits, dots, dashes or underscores.">
        <TextInput id="nu-name" autoFocus value={name} onChange={setName} />
      </Field>
      <Field id="nu-role" label="Role" hint="Admins also approve devices, manage these accounts and change the server's settings.">
        <Select id="nu-role" value={role} options={Object.keys(ROLES)} onChange={setRole} />
      </Field>
      <Field id="nu-pw" label="Password" hint="At least 12 characters. Leave empty to generate one.">
        <PasswordInput id="nu-pw" value={password} onChange={setPassword} />
      </Field>
    </Modal>
  );
}

function SetUserPassword({ name }: { name: string }) {
  const { open, close } = useModal();
  const [password, setPassword] = useState("");
  return (
    <Modal title={"New password for " + name} actions={<>
      <Button variant="text" onClick={close}>Cancel</Button>
      <Button variant="primary" onClick={async () => {
        const r = await api.setUserPassword(name, password);
        open(<UserPassword title="Password set" name={r.username} password={r.password} />);
      }}>Set password</Button>
    </>}>
      <Field id="up-pw" label="Password" hint="Leave empty to generate one. They're signed out everywhere.">
        <PasswordInput id="up-pw" value={password} onChange={setPassword} autoFocus />
      </Field>
    </Modal>
  );
}

// UserPassword shows a dashboard password once; it's stored hashed.
function UserPassword({ title, name, password }: { title: string; name: string; password: string }) {
  const copy = useCopy();
  return (
    <Modal title={title}>
      <p>Password for <b>{name}</b></p>
      <div className="secret">
        <code>{password}</code>
        <button type="button" className="icon-btn" aria-label="Copy password" data-tip="Copy password" onClick={() => copy(password, "Password copied")}>
          <Icon name="content_copy" />
        </button>
      </div>
      <div className="callout warn"><Icon name="warning" /><span>This is the only time it's shown. They sign in with it on a device you've approved.</span></div>
    </Modal>
  );
}

// SessionsTable lists open sessions: everyone's for an admin, otherwise
// the caller's own.
export function SessionsTable() {
  const me = useUser();
  const { data, error, reload } = useLoad(api.sessions);
  const toast = useToast();
  const sessions = data?.sessions ?? [];
  const admin = me?.role === "admin";
  const cols = admin ? 7 : 6;
  return (
    <div className="table-wrap">
      <table className="table">
        <thead>
          <tr>
            {admin && <th>User</th>}<th>Device</th><th>Browser</th><th>Address</th><th>Started</th><th className="grow">Last active</th>
            <th className="col-actions">Actions</th>
          </tr>
        </thead>
        <tbody>
          {error ? <EmptyRow cols={cols} icon="error">{error}</EmptyRow>
            : !data ? <EmptyRow cols={cols}>Loading…</EmptyRow>
            : sessions.map((x) => {
              const current = x.id === data.current;
              return (
                <tr key={x.id}>
                  {admin && <td>{x.user}</td>}
                  <td><span className="mono">{x.device_code || "-"}</span>{x.device_name && <span className="muted"> {x.device_name}</span>}
                    {current && <Chip health="plain">This session</Chip>}</td>
                  <td>{browserOf(x.agent)}</td>
                  <td className="mono">{x.ip}</td>
                  <td>{fmtTime(x.created)}</td>
                  <td>{timeAgo(x.last_used)}</td>
                  <td className="col-actions">
                    <IconButton icon="logout" tone="danger" label={current ? "Use Sign out for this session" : "End session"} disabled={current}
                      onClick={async () => { await api.endSession(x.id); toast("Session ended"); await reload(); }} />
                  </td>
                </tr>
              );
            })}
        </tbody>
      </table>
    </div>
  );
}
