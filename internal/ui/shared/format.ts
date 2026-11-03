// message is an error's text for a notice.
export function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}

export function fmtSize(n: number): string {
  if (!n) return "0 B";
  if (n < 1024) return n + " B";
  if (n < 1 << 20) return (n / 1024).toFixed(0) + " KB";
  if (n < 1 << 30) return (n / (1 << 20)).toFixed(1) + " MB";
  return (n / (1 << 30)).toFixed(2) + " GB";
}

// Go encodes an unset time.Time as 0001-01-01; anything that old is unset.
export function validDate(s?: string | null): Date | null {
  if (!s) return null;
  const d = new Date(s);
  return isNaN(d.getTime()) || d.getFullYear() < 2000 ? null : d;
}

// fmtTime is the time today, the date and time before that.
export function fmtTime(s?: string | null): string {
  const d = validDate(s);
  if (!d) return "-";
  if (d.toDateString() === new Date().toDateString()) return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  return d.toLocaleString([], { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

export function timeAgo(s?: string | null): string {
  const d = validDate(s);
  if (!d) return "never";
  const sec = Math.round((Date.now() - d.getTime()) / 1000);
  if (sec < 60) return "just now";
  if (sec < 3600) return Math.floor(sec / 60) + "m ago";
  if (sec < 86400) return Math.floor(sec / 3600) + "h ago";
  return Math.floor(sec / 86400) + "d ago";
}

export function plural(n: number, one: string, many = one + "s"): string {
  return `${n.toLocaleString()} ${n === 1 ? one : many}`;
}

export function storage(key: string, val?: string): string | null {
  try {
    if (val === undefined) return localStorage.getItem(key);
    localStorage.setItem(key, val);
  } catch {
    // Storage unavailable (private window, blocked site data).
  }
  return null;
}

// browserOf names the browser and system in a user agent, like "Firefox on
// Windows", for telling devices apart.
export function browserOf(ua: string): string {
  const browser = /Edg\//.test(ua) ? "Edge" : /OPR\//.test(ua) ? "Opera" : /Firefox\//.test(ua) ? "Firefox"
    : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "";
  const os = /Windows/.test(ua) ? "Windows" : /iPhone|iPad/.test(ua) ? "iOS" : /Android/.test(ua) ? "Android"
    : /Mac OS X/.test(ua) ? "macOS" : /Linux/.test(ua) ? "Linux" : "";
  return browser && os ? `${browser} on ${os}` : browser || os || "Unknown browser";
}

// generatePassword is a strong password from the browser's random source.
export function generatePassword(): string {
  const all = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
  const pick = () => all[crypto.getRandomValues(new Uint32Array(1))[0] % all.length];
  const group = () => Array.from({ length: 5 }, pick).join("");
  return [group(), group(), group(), group()].join("-");
}
