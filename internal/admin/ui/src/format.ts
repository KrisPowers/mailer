export function fmtSize(n: number): string {
  if (!n) return "0 B";
  if (n < 1024) return n + " B";
  if (n < 1 << 20) return (n / 1024).toFixed(0) + " KB";
  if (n < 1 << 30) return (n / (1 << 20)).toFixed(1) + " MB";
  return (n / (1 << 30)).toFixed(2) + " GB";
}

// Go encodes an unset time.Time as 0001-01-01; treat anything that old as unset.
function validDate(s?: string): Date | null {
  if (!s) return null;
  const d = new Date(s);
  return isNaN(d.getTime()) || d.getFullYear() < 2000 ? null : d;
}

export function fmtTime(s?: string): string {
  const d = validDate(s);
  return d ? d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }) : "—";
}

export function timeAgo(s?: string): string {
  const d = validDate(s);
  if (!d) return "never";
  const sec = Math.round((Date.now() - d.getTime()) / 1000);
  if (sec < 60) return "just now";
  if (sec < 3600) return Math.floor(sec / 60) + "m ago";
  if (sec < 86400) return Math.floor(sec / 3600) + "h ago";
  return Math.floor(sec / 86400) + "d ago";
}

export function plural(n: number, one: string, many = one + "s"): string {
  return `${n} ${n === 1 ? one : many}`;
}

export function storage(key: string, val?: string): string | null {
  try {
    if (val === undefined) return localStorage.getItem(key);
    localStorage.setItem(key, val);
  } catch {
    // storage unavailable (private window, blocked site data)
  }
  return null;
}
