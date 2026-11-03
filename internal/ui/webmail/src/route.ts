// Where the page is, kept in the URL hash: `#/f/<folder>` a folder,
// `#/flagged` everything flagged, `#/c/<category>` one category,
// `#/search/<words>` a search of every folder, `#/settings/<section>`.

export type View =
  | { kind: "folder"; folder: string }
  | { kind: "flagged" }
  | { kind: "category"; id: string }
  | { kind: "search"; q: string }
  | { kind: "settings"; section: string };

export function readHash(): View {
  const [first = "", ...rest] = location.hash.replace(/^#\/?/, "").split("/");
  const second = decodeURIComponent(rest.join("/"));
  switch (first) {
    case "flagged":
      return { kind: "flagged" };
    case "c":
      return { kind: "category", id: second };
    case "search":
      return { kind: "search", q: second };
    case "settings":
      return { kind: "settings", section: second };
  }
  return { kind: "folder", folder: first === "f" && second ? second : "INBOX" };
}

export const folderHref = (name: string) => "#/f/" + encodeURIComponent(name);

export function go(hash: string) {
  location.hash = hash.replace(/^#/, "");
}

export const viewKey = (v: View) => JSON.stringify(v);
