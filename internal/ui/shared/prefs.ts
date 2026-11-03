// How the dashboard looks, kept in this browser. Each is a data attribute on
// the page, which index.html sets before the page draws; `system` leaves it
// unset.
export type Look = "theme" | "motion" | "density";

export function loadLook(key: Look) {
  try {
    return localStorage.getItem(key) ?? "system";
  } catch {
    return "system";
  }
}

export function saveLook(key: Look, value: string) {
  const root = document.documentElement;
  if (value === "system") delete root.dataset[key];
  else root.dataset[key] = value;
  try {
    if (value === "system") localStorage.removeItem(key);
    else localStorage.setItem(key, value);
  } catch {
    // Not kept, still used until the page closes.
  }
}
