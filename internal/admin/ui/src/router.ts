import { useEffect, useState } from "react";

export const VIEWS = ["dashboard", "domains", "accounts", "aliases", "queue", "signins", "logs", "settings"] as const;
export type View = (typeof VIEWS)[number];

function current(): View {
  const v = location.hash.slice(1);
  return (VIEWS as readonly string[]).includes(v) ? (v as View) : "dashboard";
}

// useView follows the URL hash, so views are linkable and Back works.
export function useView(): View {
  const [view, setView] = useState(current);
  useEffect(() => {
    const on = () => setView(current());
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);
  return view;
}
