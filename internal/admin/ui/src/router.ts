import { useEffect, useState } from "react";

export const VIEWS = ["accounts", "queue", "logs", "access", "settings"] as const;
export type View = (typeof VIEWS)[number];

export interface Route {
  view: View;
  param: string; // e.g. the address in #accounts/kris@example.com
}

function current(): Route {
  const [v, ...rest] = decodeURIComponent(location.hash.slice(1)).split("/");
  const view = (VIEWS as readonly string[]).includes(v) ? (v as View) : "accounts";
  return { view, param: view === v ? rest.join("/") : "" };
}

export const href = (view: View, param = "") => "#" + view + (param ? "/" + encodeURIComponent(param) : "");

// useRoute follows the URL hash, so pages are linkable and Back works.
export function useRoute(): Route {
  const [route, setRoute] = useState(current);
  useEffect(() => {
    const on = () => setRoute(current());
    window.addEventListener("hashchange", on);
    return () => window.removeEventListener("hashchange", on);
  }, []);
  return route;
}
