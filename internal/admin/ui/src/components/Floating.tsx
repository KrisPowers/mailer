import {
  createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState,
  type MouseEvent, type ReactNode,
} from "react";
import { Icon, type IconName } from "./Icon";

type Kind = "menu" | "popover";

interface Open {
  anchor: HTMLElement;
  kind: Kind;
  content: ReactNode;
}

interface FloatingApi {
  anchor: HTMLElement | null;
  toggle: (anchor: HTMLElement, kind: Kind, content: ReactNode) => void;
  close: () => void;
}

const FloatingContext = createContext<FloatingApi>({ anchor: null, toggle: () => {}, close: () => {} });

export const useFloating = () => useContext(FloatingContext);

// FloatingProvider shows a single menu or popover next to the button that
// opened it. Clicking outside, Esc, scrolling or resizing closes it.
export function FloatingProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState<Open | null>(null);
  const box = useRef<HTMLDivElement>(null);

  const close = useCallback(() => setOpen(null), []);
  const toggle = useCallback((anchor: HTMLElement, kind: Kind, content: ReactNode) => {
    setOpen((cur) => (cur?.anchor === anchor ? null : { anchor, kind, content }));
  }, []);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: Event) => {
      const t = e.target as Node;
      if (!box.current?.contains(t) && !open.anchor.contains(t)) close();
    };
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && close();
    const onScroll = (e: Event) => {
      if (!box.current?.contains(e.target as Node)) close();
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("resize", close);
    window.addEventListener("scroll", onScroll, true);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("resize", close);
      window.removeEventListener("scroll", onScroll, true);
    };
  }, [open, close]);

  useLayoutEffect(() => {
    const el = box.current;
    if (!open || !el) return;
    const r = open.anchor.getBoundingClientRect();
    const w = el.offsetWidth, h = el.offsetHeight;
    const left = Math.max(8, Math.min(r.right - w, window.innerWidth - w - 8));
    let top = r.bottom + 6;
    if (top + h > window.innerHeight - 8) top = Math.max(8, r.top - h - 6);
    el.style.left = left + "px";
    el.style.top = top + "px";
  }, [open]);

  return (
    <FloatingContext.Provider value={{ anchor: open?.anchor ?? null, toggle, close }}>
      {children}
      {open && <div ref={box} className={open.kind}>{open.content}</div>}
    </FloatingContext.Provider>
  );
}

export type MenuItem = { label: string; icon: IconName; danger?: boolean; onClick: () => void } | "-";

function MenuList({ items }: { items: MenuItem[] }) {
  const { close } = useFloating();
  return (
    <>
      {items.map((it, i) => it === "-" ? <hr key={i} /> : (
        <button key={i} className={it.danger ? "danger" : ""} onClick={() => { close(); it.onClick(); }}>
          <Icon name={it.icon} />{it.label}
        </button>
      ))}
    </>
  );
}

// MoreMenu is the ⋮ button that opens a row's action menu.
export function MoreMenu({ items }: { items: () => MenuItem[] }) {
  const { anchor, toggle } = useFloating();
  const ref = useRef<HTMLButtonElement>(null);
  return (
    <button ref={ref} className={"icon-btn" + (anchor && anchor === ref.current ? " open" : "")} title="Actions" onClick={(e: MouseEvent<HTMLButtonElement>) => {
      e.stopPropagation();
      toggle(e.currentTarget, "menu", <MenuList items={items()} />);
    }}>
      <Icon name="more" />
    </button>
  );
}

interface PopoverButtonProps {
  className?: string;
  title: string;
  content: ReactNode;
  children: ReactNode;
}

export function PopoverButton({ className = "icon-btn", title, content, children }: PopoverButtonProps) {
  const { anchor, toggle } = useFloating();
  const ref = useRef<HTMLButtonElement>(null);
  return (
    <button ref={ref} className={className + (anchor && anchor === ref.current ? " open" : "")} title={title} onClick={(e) => {
      e.stopPropagation();
      toggle(e.currentTarget, "popover", content);
    }}>
      {children}
    </button>
  );
}
