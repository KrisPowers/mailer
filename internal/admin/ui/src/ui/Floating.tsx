import {
  createContext, useCallback, useContext, useEffect, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode,
} from "react";
import { Icon, type IconName } from "./Icon";

type Kind = "menu" | "popover" | "listbox";

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

// FloatingProvider shows one menu, popover or option list at a time next
// to the control that opened it. Clicking elsewhere, Esc, scrolling or
// resizing closes it, and focus returns to the control.
export function FloatingProvider({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState<Open | null>(null);
  const box = useRef<HTMLDivElement>(null);

  const close = useCallback(() => {
    setOpen((cur) => {
      cur?.anchor.focus();
      return null;
    });
  }, []);
  const toggle = useCallback((anchor: HTMLElement, kind: Kind, content: ReactNode) => {
    setOpen((cur) => (cur?.anchor === anchor ? null : { anchor, kind, content }));
  }, []);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: Event) => {
      const t = e.target as Node;
      if (!box.current?.contains(t) && !open.anchor.contains(t)) setOpen(null);
    };
    const onKey = (e: globalThis.KeyboardEvent) => e.key === "Escape" && close();
    const onScroll = (e: Event) => {
      if (!box.current?.contains(e.target as Node)) setOpen(null);
    };
    const onResize = () => setOpen(null);
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    window.addEventListener("resize", onResize);
    window.addEventListener("scroll", onScroll, true);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
      window.removeEventListener("resize", onResize);
      window.removeEventListener("scroll", onScroll, true);
    };
  }, [open, close]);

  // Option lists match their field's width and left edge; menus and
  // popovers line up with the right edge of their button. Both flip above
  // when there's no room below.
  useLayoutEffect(() => {
    const el = box.current;
    if (!open || !el) return;
    const r = open.anchor.getBoundingClientRect();
    if (open.kind === "listbox") el.style.minWidth = r.width + "px";
    const w = el.offsetWidth, h = el.offsetHeight;
    const x = open.kind === "listbox" ? r.left : r.right - w;
    el.style.left = Math.max(8, Math.min(x, window.innerWidth - w - 8)) + "px";
    el.style.top = (r.bottom + 4 + h > window.innerHeight - 8 ? Math.max(8, r.top - h - 4) : r.bottom + 4) + "px";
    el.querySelector<HTMLElement>("[aria-selected=true], [role=menuitem], [role=option]")?.focus();
  }, [open]);

  return (
    <FloatingContext.Provider value={{ anchor: open?.anchor ?? null, toggle, close }}>
      {children}
      {open && <div ref={box} className={"floating " + open.kind} onKeyDown={moveFocus}>{open.content}</div>}
    </FloatingContext.Provider>
  );
}

// moveFocus walks menu items and options with the arrow keys.
function moveFocus(e: KeyboardEvent<HTMLDivElement>) {
  if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;
  e.preventDefault();
  const items = [...e.currentTarget.querySelectorAll<HTMLElement>("[role=menuitem]:not(:disabled), [role=option]")];
  const i = items.indexOf(document.activeElement as HTMLElement);
  items[(i + (e.key === "ArrowDown" ? 1 : -1) + items.length) % items.length]?.focus();
}

export type MenuItem = { label: string; icon: IconName; danger?: boolean; onClick: () => void } | "-";

// Menu is the ⋮ button that opens a row's other actions.
export function Menu({ items, label = "More actions" }: { items: () => MenuItem[]; label?: string }) {
  const { anchor, toggle, close } = useFloating();
  const ref = useRef<HTMLButtonElement>(null);
  return (
    <button ref={ref} type="button" aria-label={label} data-tip={label} aria-haspopup="menu" aria-expanded={!!anchor && anchor === ref.current}
      className={"icon-btn" + (anchor && anchor === ref.current ? " active" : "")}
      onClick={(e) => toggle(e.currentTarget, "menu", (
        <div role="menu">
          {items().map((it, i) => it === "-" ? <hr key={i} /> : (
            <button key={i} type="button" role="menuitem" className={it.danger ? "danger" : ""} onClick={() => { close(); it.onClick(); }}>
              <Icon name={it.icon} size={18} />{it.label}
            </button>
          ))}
        </div>
      ))}>
      <Icon name="more_vert" />
    </button>
  );
}

// PopoverButton opens content in a popover from any button-like trigger.
export function PopoverButton({ className, label, content, children }: {
  className?: string;
  label: string;
  content: ReactNode;
  children: ReactNode;
}) {
  const { anchor, toggle } = useFloating();
  const ref = useRef<HTMLButtonElement>(null);
  const isOpen = !!anchor && anchor === ref.current;
  return (
    <button ref={ref} type="button" aria-label={label} data-tip={label} aria-expanded={isOpen}
      className={(className ?? "icon-btn") + (isOpen ? " active" : "")}
      onClick={(e) => toggle(e.currentTarget, "popover", content)}>
      {children}
    </button>
  );
}
