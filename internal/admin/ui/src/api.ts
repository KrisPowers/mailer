import type {
  Account, Aliases, Checks, ConnectResult, Credentials, Overview, QueueItem, Settings, TailscaleResult,
} from "./types";

// Every request carries X-Mailer: the server refuses state changes without
// it, which other sites can't send cross-origin.
async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { "X-Mailer": "1" };
  const init: RequestInit = { method, headers };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  const r = await fetch(path, init);
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error((data as { error?: string }).error || r.statusText);
  return data as T;
}

export const api = {
  overview: () => call<Overview>("GET", "/api/overview"),
  check: () => call<Checks>("POST", "/api/check"),
  connectCloudflare: (token: string, zone: string) =>
    call<ConnectResult>("POST", "/api/cloudflare/connect", { token, zone }),
  enableTailscale: () => call<TailscaleResult>("POST", "/api/tailscale/enable"),
  accounts: () => call<Account[]>("GET", "/api/accounts"),
  createAccount: (email: string) => call<Credentials>("POST", "/api/accounts", { email }),
  resetPassword: (email: string) => call<Credentials>("POST", "/api/accounts/password", { email }),
  deleteAccount: (email: string) => call<{ ok: boolean }>("POST", "/api/accounts/delete", { email }),
  aliases: () => call<Aliases>("GET", "/api/aliases"),
  saveAliases: (a: Aliases) => call<Aliases>("PUT", "/api/aliases", a),
  queue: () => call<QueueItem[]>("GET", "/api/queue"),
  retryQueue: () => call<unknown>("POST", "/api/queue/retry"),
  logs: () => call<string[]>("GET", "/api/logs"),
  settings: () => call<Settings>("GET", "/api/settings"),
  saveSettings: (s: Settings) => call<Settings>("PUT", "/api/settings", s),
};
