import { useEffect, useLayoutEffect, useRef, useState } from "react";

interface Tip {
  text: string;
  rect: DOMRect;
}

// TooltipLayer shows the data-tip of whatever is hovered or focused, drawn
// above everything so no scrolling container cuts it off.
export function TooltipLayer() {
  const [tip, setTip] = useState<Tip | null>(null);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const target = (e: Event) => (e.target as Element | null)?.closest?.("[data-tip]") as HTMLElement | null;
    const show = (e: Event) => {
      const el = target(e);
      if (el?.dataset.tip) setTip({ text: el.dataset.tip, rect: el.getBoundingClientRect() });
    };
    const hide = (e: Event) => {
      if (target(e)) setTip(null);
    };
    const clear = () => setTip(null);
    document.addEventListener("mouseover", show);
    document.addEventListener("focusin", show);
    document.addEventListener("mouseout", hide);
    document.addEventListener("focusout", hide);
    document.addEventListener("mousedown", clear);
    window.addEventListener("scroll", clear, true);
    return () => {
      document.removeEventListener("mouseover", show);
      document.removeEventListener("focusin", show);
      document.removeEventListener("mouseout", hide);
      document.removeEventListener("focusout", hide);
      document.removeEventListener("mousedown", clear);
      window.removeEventListener("scroll", clear, true);
    };
  }, []);

  // Centred under the element, kept inside the window, flipped above it
  // near the bottom edge.
  useLayoutEffect(() => {
    const el = box.current;
    if (!tip || !el) return;
    const { rect } = tip;
    const w = el.offsetWidth, h = el.offsetHeight;
    const left = Math.min(Math.max(8, rect.left + rect.width / 2 - w / 2), window.innerWidth - w - 8);
    const top = rect.bottom + 6 + h > window.innerHeight - 8 ? rect.top - h - 6 : rect.bottom + 6;
    el.style.left = left + "px";
    el.style.top = top + "px";
  }, [tip]);

  return tip ? <div ref={box} className="tooltip" role="tooltip">{tip.text}</div> : null;
}
