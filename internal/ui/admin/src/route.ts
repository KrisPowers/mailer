// Where the page is, kept in the URL hash: `#/` the accounts, `#/new` a new
// one, `#/accounts/<address>/<page>` one account; `#/people` and
// `#/people/new`; `#/setup/<step>`, `#/settings/<section>` and
// `#/help/<guide>/<heading>`; every other page is `#/<page>`.

export const VIEWS = ["accounts", "new", "traffic", "flagged", "status", "logs", "devices", "people", "setup", "settings", "help"] as const;
export type View = (typeof VIEWS)[number];

export interface Place {
  view: View;
  id: string;
  // A page within one account, or a heading within a guide.
  page: string;
}

export function readHash(): Place {
  const [first = "", second = "", third = ""] = location.hash.replace(/^#\/?/, "").split("/").map(decodeURIComponent);
  const view = (VIEWS as readonly string[]).includes(first) ? (first as View) : "accounts";
  return { view, id: view === first ? second : "", page: view === first ? third : "" };
}

export function go(hash: string) {
  location.hash = hash.replace(/^#/, "");
}

export const accountHref = (email: string, page = "") => `#/accounts/${encodeURIComponent(email)}${page ? "/" + page : ""}`;
