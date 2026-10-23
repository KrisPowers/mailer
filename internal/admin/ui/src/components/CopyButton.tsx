import { Icon } from "./Icon";
import { useCopy } from "./Toasts";

export function CopyButton({ text, label = "Copied" }: { text: string; label?: string }) {
  const copy = useCopy();
  return (
    <button className="icon-btn" title="Copy" onClick={(e) => { e.stopPropagation(); copy(text, label); }}>
      <Icon name="copy" />
    </button>
  );
}
