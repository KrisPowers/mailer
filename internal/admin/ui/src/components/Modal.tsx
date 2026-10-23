import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { Icon } from "./Icon";

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

interface Props {
  title: ReactNode;
  wide?: boolean;
  footer?: ReactNode;
  children: ReactNode;
}

export function Modal({ title, wide, footer, children }: Props) {
  const ref = useRef<HTMLDialogElement>(null);
  const { close } = useModal();
  useEffect(() => {
    const d = ref.current;
    if (d && !d.open) d.showModal();
  }, []);
  return (
    <dialog ref={ref} className={wide ? "wide" : ""} onClose={close}>
      <div className="modal-head">
        <h2>{title}</h2>
        <button className="icon-btn" title="Close" onClick={close}><Icon name="x" /></button>
      </div>
      <div className="modal-body">{children}</div>
      {footer && <div className="modal-foot">{footer}</div>}
    </dialog>
  );
}

export function CloseButton({ label = "Close", primary }: { label?: string; primary?: boolean }) {
  const { close } = useModal();
  return <button className={"btn" + (primary ? " primary" : "")} onClick={close}>{label}</button>;
}
