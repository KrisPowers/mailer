import { useState, type ButtonHTMLAttributes, type MouseEvent } from "react";
import { useToast } from "./Toasts";

type Props = Omit<ButtonHTMLAttributes<HTMLButtonElement>, "onClick"> & {
  onClick: (e: MouseEvent<HTMLButtonElement>) => Promise<unknown> | unknown;
};

// AsyncButton disables itself while its handler runs and turns a thrown
// error into a toast.
export function AsyncButton({ onClick, children, disabled, ...rest }: Props) {
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  return (
    <button
      {...rest}
      disabled={disabled || busy}
      onClick={async (e) => {
        setBusy(true);
        try {
          await onClick(e);
        } catch (err) {
          toast((err as Error).message, true);
        } finally {
          setBusy(false);
        }
      }}
    >
      {busy ? "Working…" : children}
    </button>
  );
}
