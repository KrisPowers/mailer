import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { api } from "../api";
import { Button } from "../ui/Button";
import { SearchField } from "../ui/Field";
import { Toolbar } from "../ui/Page";
import { Toggle } from "../ui/Toggle";
import { useCopy } from "../ui/Toast";

const ERROR_LINE = /error|failed|denied|reject|refused|timeout/i;
const LINE = /^(\d{4}\/\d\d\/\d\d) (\d\d:\d\d:\d\d) (.*)$/;

export function Logs() {
  const [lines, setLines] = useState<string[]>([]);
  const [q, setQ] = useState("");
  const [errorsOnly, setErrorsOnly] = useState(false);
  const [paused, setPaused] = useState(false);
  const box = useRef<HTMLDivElement>(null);
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

  // Keep following new lines only while scrolled to the bottom.
  useLayoutEffect(() => {
    const b = box.current;
    if (b && stick.current) b.scrollTop = b.scrollHeight;
  }, [lines, q, errorsOnly]);

  const query = q.toLowerCase();
  const shown = lines.filter((l) => (!errorsOnly || ERROR_LINE.test(l)) && (!query || l.toLowerCase().includes(query)));

  return (
    <>
      <Toolbar actions={<>
        <Button icon={paused ? "play_arrow" : "pause"} onClick={() => setPaused(!paused)}>{paused ? "Resume" : "Pause"}</Button>
        <Button icon="content_copy" onClick={() => copy(shown.join("\n"), "Logs copied")}>Copy</Button>
      </>}>
        <SearchField value={q} onChange={setQ} placeholder="Filter" />
        <Toggle checked={errorsOnly} onChange={setErrorsOnly}>Only show errors</Toggle>
      </Toolbar>
      <div className="log-view" ref={box} role="log" onScroll={(e) => {
        const b = e.currentTarget;
        stick.current = b.scrollTop + b.clientHeight >= b.scrollHeight - 30;
      }}>
        {shown.length ? shown.map((l, i) => {
          const m = l.match(LINE);
          return (
            <div key={i} className={"log-line" + (ERROR_LINE.test(l) ? " error" : "")}>
              {m ? <><span className="log-time">{m[2]}</span><span>{m[3]}</span></> : <span>{l}</span>}
            </div>
          );
        }) : <div className="muted">{lines.length ? "No lines match." : "Nothing logged yet."}</div>}
      </div>
    </>
  );
}
