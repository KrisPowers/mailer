import { useSyncExternalStore } from "react";
import { storage } from "./format";
import type { DashUser, Tokens } from "./types";

// The device ID is this browser's: given by the server on the first
// visit and kept in localStorage. Without storage it lasts as long as the
// page, and the browser asks for approval again next time.
const DEVICE_KEY = "mailer.device";
let memDevice = "";

export const deviceId = () => storage(DEVICE_KEY) ?? memDevice;

export function setDeviceId(id: string) {
  memDevice = id;
  storage(DEVICE_KEY, id);
}

// The token lives in memory only. The refresh token is an HttpOnly cookie
// the page can't read; it brings the session back after a reload.
let token = "";
let user: DashUser | null = null;
let timer: ReturnType<typeof setTimeout> | undefined;
const listeners = new Set<() => void>();

const emit = () => listeners.forEach((fn) => fn());

export function headers(): Record<string, string> {
  const h: Record<string, string> = { "X-Mailer": "1" };
  const id = deviceId();
  if (id) h["X-Device"] = id;
  if (token) h["Authorization"] = "Bearer " + token;
  return h;
}

export function signedIn(t: Tokens) {
  token = t.token;
  user = t.user;
  clearTimeout(timer);
  // Refresh a minute before the token runs out, while the page is open.
  const ms = new Date(t.expires).getTime() - Date.now() - 60_000;
  timer = setTimeout(() => { refresh(); }, Math.max(ms, 5_000));
  emit();
}

// signedOut forgets the token; listeners hear about it only when someone
// was signed in.
export function signedOut() {
  const was = user !== null;
  token = "";
  user = null;
  clearTimeout(timer);
  if (was) emit();
}

let refreshing: Promise<boolean> | null = null;

// refresh trades the refresh cookie for a new token. Calls made while one
// is under way share it.
export function refresh(): Promise<boolean> {
  refreshing ??= (async () => {
    try {
      const r = await fetch("/api/auth/refresh", { method: "POST", headers: { "X-Mailer": "1", "X-Device": deviceId() } });
      if (!r.ok) {
        signedOut();
        return false;
      }
      signedIn(await r.json());
      return true;
    } catch {
      return false;
    } finally {
      refreshing = null;
    }
  })();
  return refreshing;
}

const subscribe = (fn: () => void) => {
  listeners.add(fn);
  return () => { listeners.delete(fn); };
};

export const currentUser = () => user;

// useUser is the signed-in account, or null.
export const useUser = () => useSyncExternalStore(subscribe, currentUser);

export const onSessionChange = subscribe;
