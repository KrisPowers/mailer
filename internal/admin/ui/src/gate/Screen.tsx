import type { ReactNode } from "react";
import { Icon, type IconName } from "../ui/Icon";

// Screen is the card every page before the dashboard sits on, under the
// dashboard's brand.
export function Screen({ icon, title, wide, children }: { icon?: IconName; title?: string; wide?: boolean; children: ReactNode }) {
  return (
    <div className="gate">
      <div className={"gate-card" + (wide ? " wide" : "")}>
        <div className="gate-brand"><Icon name="mail" size={26} /><span>mailer</span></div>
        <div className="gate-body">
          {title && <h1 className="gate-title">{icon && <Icon name={icon} size={24} />}{title}</h1>}
          {children}
        </div>
      </div>
    </div>
  );
}
