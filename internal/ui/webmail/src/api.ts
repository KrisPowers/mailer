import type { Addr, Detail, Draft, Folder, Me, Page, Prefs, Ref } from "./types";

// Every request carries X-Mailer, which the server requires for changes and
// other sites can't send; the session is an HttpOnly cookie. A refused
// session tells the page, which shows the sign-in.
async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { "X-Mailer": "1" };
  const init: RequestInit = { method, headers, credentials: "same-origin" };
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  const r = await fetch(path, init);
  if (r.status === 401 && path !== "/api/login") dispatchEvent(new Event("signed-out"));
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error((data as { error?: string }).error || r.statusText);
  return data as T;
}

const query = (params: Record<string, string | number | undefined>) =>
  "?" + Object.entries(params).filter(([, v]) => v !== undefined && v !== "").map(([k, v]) => `${k}=${encodeURIComponent(String(v))}`).join("&");

export interface ListQuery {
  folder?: string;
  all?: boolean;
  q?: string;
  show?: string;
  offset?: number;
  limit?: number;
}

export const msgPath = (kind: "body" | "part" | "raw", ref: Ref, extra: Record<string, string | number> = {}) =>
  `/api/message/${kind}` + query({ folder: ref.folder, uid: ref.uid, ...extra });

export const api = {
  session: () => call<{ signed_in: boolean; email?: string }>("GET", "/api/session"),
  login: (email: string, password: string) => call<{ email: string }>("POST", "/api/login", { email, password }),
  logout: () => call<{ ok: boolean }>("POST", "/api/logout"),
  me: () => call<Me>("GET", "/api/me"),
  savePrefs: (p: Prefs) => call<Prefs>("PUT", "/api/prefs", p),
  folders: () => call<Folder[]>("GET", "/api/folders"),
  createFolder: (name: string) => call<Folder[]>("POST", "/api/folders", { name }),
  renameFolder: (from: string, to: string) => call<Folder[]>("POST", "/api/folders/rename", { from, to }),
  deleteFolder: (name: string) => call<Folder[]>("POST", "/api/folders/delete", { name }),
  emptyFolder: (name: string) => call<Folder[]>("POST", "/api/folders/empty", { name }),
  messages: (q: ListQuery) => call<Page>("GET", "/api/messages" + query({
    folder: q.folder, all: q.all ? 1 : undefined, q: q.q, show: q.show, offset: q.offset, limit: q.limit,
  })),
  message: (ref: Ref, edit = false) => call<Detail>("GET", "/api/message" + query({ folder: ref.folder, uid: ref.uid, edit: edit ? 1 : undefined })),
  flags: (folder: string, uids: number[], add: string[], remove: string[] = []) =>
    call<{ ok: boolean }>("POST", "/api/messages/flags", { folder, uids, add, remove }),
  move: (folder: string, uids: number[], to: string) => call<{ ok: boolean }>("POST", "/api/messages/move", { folder, uids, to }),
  remove: (folder: string, uids: number[]) => call<{ ok: boolean }>("POST", "/api/messages/delete", { folder, uids }),
  send: (d: Draft) => call<{ ok: boolean }>("POST", "/api/send", d),
  saveDraft: (d: Draft) => call<Ref>("POST", "/api/drafts", d),
  runRules: (folder: string) => call<{ matched: number }>("POST", "/api/rules/run", { folder }),
  contacts: () => call<Addr[]>("GET", "/api/contacts"),
};
