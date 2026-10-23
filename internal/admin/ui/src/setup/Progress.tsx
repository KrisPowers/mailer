import { Icon } from "../components/Icon";
import type { Step } from "../types";

// Progress shows the result of each Cloudflare setup step.
export function Progress({ steps, error }: { steps?: Step[]; error?: string }) {
  return (
    <ul className="progress">
      {(steps ?? []).map((s, i) => (
        <li key={i} className={s.ok ? "ok" : "err"}>
          <Icon name={s.ok ? "check" : "x"} />
          {s.name}
          <span className="d">{s.detail ?? ""}</span>
        </li>
      ))}
      {error && <li className="err"><Icon name="alert" />{error}</li>}
    </ul>
  );
}
