import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { Button, IconButton } from "./Button";

interface ModalApi {
  open: (node: ReactNode) => void;
  close: () => void;
}

const ModalContext = createContext<ModalApi>({ open: () => {}, close: () => {} });

export const useModal = () => useContext(ModalContext);

// ModalProvider shows one dialog at a time; opening another replaces it.
export function ModalProvider({ children }: { children: ReactNode }) {
  const [current, setCurrent] = useState<{ key: number; node: ReactNode } | null>(null);
  const open = useCallback((node: ReactNode) => setCurrent((c) => ({ key: (c?.key ?? 0) + 1, node })), []);
  const close = useCallback(() => setCurrent(null), []);
  return (
    <ModalContext.Provider value={{ open, close }}>
      {children}
      {current && <div key={current.key}>{current.node}</div>}
    </ModalContext.Provider>
  );
}

// Modal is a dialog over a dimmed page. Esc or the scrim closes it, focus
// starts inside it and Tab stays inside it.
export function Modal({ title, wide, actions, children }: { title: ReactNode; wide?: boolean; actions?: ReactNode; children: ReactNode }) {
  const { close } = useModal();
  const box = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const before = document.activeElement as HTMLElement | null;
    const first = box.current?.querySelector<HTMLElement>("[autofocus], input, .modal-body button, .modal-actions .primary");
    (first ?? box.current)?.focus();
    return () => before?.focus();
  }, []);
  return (
    <div className="scrim" onMouseDown={(e) => e.target === e.currentTarget && close()}>
      <div ref={box} className={"modal" + (wide ? " wide" : "")} role="dialog" aria-modal="true" tabIndex={-1}
        onKeyDown={(e) => {
          if (e.key === "Escape") close();
          if (e.key !== "Tab" || !box.current) return;
          const f = [...box.current.querySelectorAll<HTMLElement>("button:not(:disabled), input, [tabindex='0']")];
          if (!f.length) return;
          if (e.shiftKey && document.activeElement === f[0]) { e.preventDefault(); f[f.length - 1].focus(); }
          if (!e.shiftKey && document.activeElement === f[f.length - 1]) { e.preventDefault(); f[0].focus(); }
        }}>
        <div className="modal-head">
          <h2>{title}</h2>
          <IconButton icon="close" label="Close" onClick={close} />
        </div>
        <div className="modal-body">{children}</div>
        <div className="modal-actions">{actions ?? <Button variant="text" onClick={close}>Close</Button>}</div>
      </div>
    </div>
  );
}

// Confirm asks before something that can't be undone.
export function Confirm({ title, message, label, onYes }: { title: string; message: ReactNode; label: string; onYes: () => Promise<void> }) {
  const { close } = useModal();
  return (
    <Modal title={title} actions={<>
      <Button variant="text" onClick={close}>Cancel</Button>
      <Button variant="danger" onClick={async () => { await onYes(); close(); }}>{label}</Button>
    </>}>
      <p>{message}</p>
    </Modal>
  );
}
