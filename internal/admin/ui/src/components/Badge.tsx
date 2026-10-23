import type { ReactNode } from "react";

export type Health = "ok" | "warn" | "bad" | "none";

export const healthText: Record<Health, string> = { ok: "Healthy", warn: "Attention", bad: "Problem", none: "Unknown" };

export function Badge({ kind, children }: { kind: Health | "plain"; children: ReactNode }) {
  return <span className={"badge " + kind}>{children}</span>;
}
