import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { Icon } from "./Icon";

type Tone = "ok" | "bad";
type ToastFn = (msg: string, tone?: Tone) => void;

const ToastContext = createContext<ToastFn>(() => {});

export const useToast = () => useContext(ToastContext);

let nextId = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<{ id: number; msg: string; tone: Tone }[]>([]);
  const toast = useCallback<ToastFn>((msg, tone = "ok") => {
    const id = ++nextId;
    setToasts((t) => [...t, { id, msg, tone }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), tone === "bad" ? 7000 : 3000);
  }, []);
  return (
    <ToastContext.Provider value={toast}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {toasts.map((t) => (
          <div key={t.id} className={"toast " + t.tone}>
            <Icon name={t.tone === "bad" ? "error" : "check_circle"} />
            <span>{t.msg}</span>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

// useCopy copies text and confirms it with a toast.
export function useCopy() {
  const toast = useToast();
  return useCallback(async (text: string, what = "Copied") => {
    try {
      await navigator.clipboard.writeText(text);
      toast(what);
    } catch {
      toast("Couldn't copy to the clipboard", "bad");
    }
  }, [toast]);
}
