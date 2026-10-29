import { headers, refresh } from "./session";
import type {
  Account, Aliases, AuthStatus, Checks, ConnectResult, Credentials, DashUser, DeviceList, FlaggedMessage, Locks, MailEntry, NewDevice, Overview,
  QueueItem, ServerSettings, SessionList, Settings, SignIns, Step, TailscaleResult, Tokens,
} from "./types";

// Every request carries X-Mailer, which the server requires for state
// changes and other sites can't send cross-origin, the device ID, and the
// token once signed in. A request refused for an expired token is retried
// once after refreshing it.
async function call<T>(method: string, path: string, body?: unknown, retry = true): Promise<T> {
  const h = headers();
  const init: RequestInit = { method, headers: h };
  if (body !== undefined) {
    h["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  const r = await fetch(path, init);
  if (r.status === 401 && retry && await refresh()) return call<T>(method, path, body, false);
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error((data as { error?: string }).error || r.statusText);
  return data as T;
}

// Calls made before signing in, never retried.
const pub = <T>(method: string, path: string, body?: unknown) => call<T>(method, path, body, false);

export interface SetupRequest {
  username: string;
  password: string;
  tailscale: { hostname: string; auth_key: string };
  remote: { enabled: boolean; subdomain: string };
}

export const api = {
  authStatus: () => pub<AuthStatus>("GET", "/api/auth/status"),
  newDevice: () => pub<NewDevice>("POST", "/api/auth/device"),
  setup: (req: SetupRequest) => pub<Tokens>("POST", "/api/auth/setup", req),
  login: (username: string, password: string) => pub<Tokens>("POST", "/api/auth/login", { username, password }),
  logout: () => pub<{ ok: boolean }>("POST", "/api/auth/logout"),
  changePassword: (current: string, password: string) => call<{ ok: boolean }>("POST", "/api/auth/password", { current, password }),
  sessions: () => call<SessionList>("GET", "/api/auth/sessions"),
  endSession: (id: string) => call<{ ok: boolean }>("POST", "/api/auth/sessions/end", { id }),
  users: () => call<DashUser[]>("GET", "/api/auth/users"),
  addUser: (username: string, password: string, role: string) =>
    call<{ user: DashUser; password: string }>("POST", "/api/auth/users", { username, password, role }),
  setUserPassword: (username: string, password: string) =>
    call<{ username: string; password: string }>("POST", "/api/auth/users/password", { username, password }),
  setUserRole: (username: string, role: string) => call<{ ok: boolean }>("POST", "/api/auth/users/role", { username, role }),
  setUserDisabled: (username: string, disabled: boolean) =>
    call<{ ok: boolean }>("POST", "/api/auth/users/disable", { username, disabled }),
  deleteUser: (username: string) => call<{ ok: boolean }>("POST", "/api/auth/users/delete", { username }),
  devices: () => call<DeviceList>("GET", "/api/auth/devices"),
  approveDevice: (key: string, name: string) => call<{ ok: boolean }>("POST", "/api/auth/devices/approve", { key, name }),
  renameDevice: (key: string, name: string) => call<{ ok: boolean }>("POST", "/api/auth/devices/rename", { key, name }),
  removeDevice: (key: string) => call<{ ok: boolean }>("POST", "/api/auth/devices/delete", { key }),
  server: () => call<ServerSettings>("GET", "/api/server"),
  saveServer: (s: ServerSettings) => call<ServerSettings>("PUT", "/api/server", s),

  overview: () => call<Overview>("GET", "/api/overview"),
  check: () => call<Checks>("POST", "/api/check"),
  signInCloudflare: () => call<ConnectResult & { url?: string }>("POST", "/api/cloudflare/signin"),
  signInCloudflareWait: () => call<ConnectResult>("POST", "/api/cloudflare/signin/wait"),
  connectCloudflare: (token: string, zone: string) =>
    call<ConnectResult>("POST", "/api/cloudflare/connect", { token, zone }),
  enableTailscale: () => call<TailscaleResult>("POST", "/api/tailscale/enable"),
  accounts: () => call<Account[]>("GET", "/api/accounts"),
  createAccount: (email: string, password: string) => call<Credentials>("POST", "/api/accounts", { email, password }),
  setPassword: (email: string, password: string) => call<Credentials>("POST", "/api/accounts/password", { email, password }),
  deleteAccount: (email: string) => call<{ ok: boolean }>("POST", "/api/accounts/delete", { email }),
  setLocks: (email: string, locks: Locks) => call<Locks>("POST", "/api/accounts/locks", { email, ...locks }),
  mail: () => call<MailEntry[]>("GET", "/api/mail"),
  flagged: () => call<FlaggedMessage[]>("GET", "/api/flagged"),
  clearFlag: (m: FlaggedMessage) => call<{ ok: boolean }>("POST", "/api/flagged/clear", { account: m.account, mailbox: m.mailbox, uid: m.uid }),
  trashFlagged: (m: FlaggedMessage) => call<{ ok: boolean }>("POST", "/api/flagged/trash", { account: m.account, mailbox: m.mailbox, uid: m.uid }),
  aliases: () => call<Aliases>("GET", "/api/aliases"),
  saveAliases: (a: Aliases) => call<Aliases>("PUT", "/api/aliases", a),
  queue: () => call<QueueItem[]>("GET", "/api/queue"),
  retryQueue: () => call<unknown>("POST", "/api/queue/retry"),
  logs: () => call<string[]>("GET", "/api/logs"),
  signIns: () => call<SignIns>("GET", "/api/signins"),
  unblock: (ip: string) => call<{ ok: boolean }>("POST", "/api/signins/unblock", { ip }),
  testSignIn: (email: string, password: string) => call<{ steps: Step[] }>("POST", "/api/signins/test", { email, password }),
  settings: () => call<Settings>("GET", "/api/settings"),
  saveSettings: (s: Settings) => call<Settings>("PUT", "/api/settings", s),
};
