import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { Icon } from "./Icon";

interface Toast {
  id: number;
  msg: string;
  bad: boolean;
}

type ToastFn = (msg: string, bad?: boolean) => void;

const ToastContext = createContext<ToastFn>(() => {});

export const useToast = () => useContext(ToastContext);

let nextId = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [toasts, setToasts] = useState<Toast[]>([]);

  const toast = useCallback<ToastFn>((msg, bad = false) => {
    const id = ++nextId;
    setToasts((t) => [...t, { id, msg, bad }]);
    setTimeout(() => setToasts((t) => t.filter((x) => x.id !== id)), bad ? 6000 : 2800);
  }, []);

  return (
    <ToastContext.Provider value={toast}>
      {children}
      <div className="toasts" role="status">
        {toasts.map((t) => (
          <div key={t.id} className={"toast" + (t.bad ? " bad" : "")}>
            <Icon name={t.bad ? "alert" : "check"} />
            <span>{t.msg}</span>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

// useCopy copies text to the clipboard and confirms with a toast.
export function useCopy() {
  const toast = useToast();
  return useCallback(async (text: string, what = "Copied") => {
    try {
      await navigator.clipboard.writeText(text);
      toast(what);
    } catch {
      toast("Couldn't copy — select and copy manually", true);
    }
  }, [toast]);
}
