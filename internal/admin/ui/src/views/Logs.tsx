import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { api } from "../api";
import { Icon } from "../components/Icon";
import { PageHead, Panel, Search } from "../components/Layout";
import { useCopy } from "../components/Toasts";

const ERROR_LINE = /error|failed|denied|reject/i;

export function Logs() {
  const [lines, setLines] = useState<string[]>([]);
  const [q, setQ] = useState("");
  const [paused, setPaused] = useState(false);
  const box = useRef<HTMLPreElement>(null);
  const stick = useRef(true);
  const copy = useCopy();

  useEffect(() => {
    if (paused) return;
    let live = true;
    const tick = () => api.logs().then((l) => live && setLines(l)).catch(() => {});
    tick();
    const t = setInterval(tick, 3000);
    return () => { live = false; clearInterval(t); };
  }, [paused]);

  // Keep following new lines only while the reader is at the bottom.
  useLayoutEffect(() => {
    const b = box.current;
    if (b && stick.current) b.scrollTop = b.scrollHeight;
  }, [lines, q]);

  const query = q.toLowerCase();
  const shown = lines.filter((l) => !query || l.toLowerCase().includes(query));

  return (
    <>
      <PageHead title="Logs" sub={paused ? "Paused" : "Live"}>
        <Search value={q} onChange={setQ} placeholder="Filter" />
        <button className="icon-btn" title={paused ? "Resume" : "Pause"} onClick={() => setPaused(!paused)}>
          <Icon name={paused ? "play" : "pause"} />
        </button>
        <button className="icon-btn" title="Copy all" onClick={() => copy(lines.join("\n"), "Logs copied")}>
          <Icon name="copy" />
        </button>
      </PageHead>
      <Panel>
        <pre className="logbox" ref={box} onScroll={(e) => {
          const b = e.currentTarget;
          stick.current = b.scrollTop + b.clientHeight >= b.scrollHeight - 30;
        }}>
          {shown.map((l, i) => <div key={i} className={ERROR_LINE.test(l) ? "err" : ""}>{l}</div>)}
        </pre>
      </Panel>
    </>
  );
}
