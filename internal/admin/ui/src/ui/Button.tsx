import { useState, type ButtonHTMLAttributes, type MouseEvent, type ReactNode } from "react";
import { Icon, type IconName } from "./Icon";
import { useToast } from "./Toast";

type Variant = "primary" | "secondary" | "text" | "danger";

interface ButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "onClick"> {
  variant?: Variant;
  icon?: IconName;
  small?: boolean;
  // A returned promise keeps the button busy until it settles; a thrown
  // error becomes a toast.
  onClick?: (e: MouseEvent<HTMLButtonElement>) => unknown;
  children: ReactNode;
}

export function Button({ variant = "secondary", icon, small, onClick, disabled, className, children, ...rest }: ButtonProps) {
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  return (
    <button type="button" {...rest} disabled={disabled || busy}
      className={["btn", variant, small && "small", busy && "busy", className].filter(Boolean).join(" ")}
      onClick={async (e) => {
        const r = onClick?.(e);
        if (!(r instanceof Promise)) return;
        setBusy(true);
        try {
          await r;
        } catch (err) {
          toast((err as Error).message, "bad");
        } finally {
          setBusy(false);
        }
      }}>
      {busy ? <span className="spinner" aria-hidden="true" /> : icon && <Icon name={icon} size={18} />}
      <span>{children}</span>
    </button>
  );
}

interface IconButtonProps extends Omit<ButtonHTMLAttributes<HTMLButtonElement>, "onClick"> {
  icon: IconName;
  label: string; // tooltip and accessible name
  tone?: "accent" | "danger";
  active?: boolean;
  onClick?: (e: MouseEvent<HTMLButtonElement>) => unknown;
}

export function IconButton({ icon, label, tone, active, onClick, className, disabled, ...rest }: IconButtonProps) {
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  return (
    <button type="button" {...rest} aria-label={label} data-tip={label} disabled={disabled || busy}
      className={["icon-btn", tone, active && "active", busy && "busy", className].filter(Boolean).join(" ")}
      onClick={async (e) => {
        const r = onClick?.(e);
        if (!(r instanceof Promise)) return;
        setBusy(true);
        try {
          await r;
        } catch (err) {
          toast((err as Error).message, "bad");
        } finally {
          setBusy(false);
        }
      }}>
      <Icon name={icon} />
    </button>
  );
}

// LinkButton is a text link that runs an action, like Docker's
// "Give feedback" beside a page title.
export function LinkButton({ onClick, icon, children }: { onClick: () => void; icon?: IconName; children: ReactNode }) {
  return (
    <button type="button" className="link-btn" onClick={onClick}>
      {children}{icon && <Icon name={icon} size={14} />}
    </button>
  );
}
