import { useRef } from "react";
import { useFloating } from "./Floating";
import { Icon } from "./Icon";

interface Props {
  id?: string;
  value: string;
  options: string[];
  onChange: (v: string) => void;
  disabled?: boolean;
  label?: string; // accessible name when there's no <label for>
}

// Select is a drop-down list drawn by the page, not the browser.
export function Select({ id, value, options, onChange, disabled, label }: Props) {
  const { anchor, toggle, close } = useFloating();
  const ref = useRef<HTMLButtonElement>(null);
  const isOpen = !!anchor && anchor === ref.current;
  const openList = (el: HTMLButtonElement) => toggle(el, "listbox", (
    <div role="listbox" aria-label={label}>
      {options.map((o) => (
        <button key={o} type="button" role="option" aria-selected={o === value}
          onClick={() => { close(); if (o !== value) onChange(o); }}>
          <span>{o}</span>{o === value && <Icon name="check" size={18} />}
        </button>
      ))}
    </div>
  ));
  return (
    <button ref={ref} id={id} type="button" className={"select" + (isOpen ? " open" : "")} disabled={disabled || !options.length}
      aria-haspopup="listbox" aria-expanded={isOpen} aria-label={label}
      onClick={(e) => openList(e.currentTarget)}
      onKeyDown={(e) => {
        if ((e.key === "ArrowDown" || e.key === "ArrowUp") && !isOpen) {
          e.preventDefault();
          openList(e.currentTarget);
        }
      }}>
      <span>{value || "Choose…"}</span>
      <Icon name="expand_more" />
    </button>
  );
}
