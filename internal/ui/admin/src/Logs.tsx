import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { Icon } from "../../shared/Icon";
import { PageHead, Switch, useNotify } from "../../shared/ui";
import { api } from "./api";

const ERROR_LINE = /error|failed|denied|reject|refused|timeout/i;
const LINE = /^(\d{4}\/\d\d\/\d\d) (\d\d:\d\d:\d\d) (.*)$/;

// Logs is the server's recent activity, following new lines while scrolled to
// the bottom.
export function Logs() {
  const [lines, setLines] = useState<string[]>([]);
  const [q, setQ] = useState("");
  const [errorsOnly, setErrorsOnly] = useState(false);
  const [paused, setPaused] = useState(false);
  const box = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const notify = useNotify();

  useEffect(() => {
    if (paused) return;
    let live = true;
    const tick = () => api.logs().then((l) => live && setLines(l)).catch(() => {});
    tick();
    const t = setInterval(tick, 3000);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [paused]);

  useLayoutEffect(() => {
    const b = box.current;
    if (b && stick.current) b.scrollTop = b.scrollHeight;
  }, [lines, q, errorsOnly]);

  const needle = q.trim().toLowerCase();
  const shown = lines.filter((l) => (!errorsOnly || ERROR_LINE.test(l)) && (!needle || l.toLowerCase().includes(needle)));

  return (
    <>
      <PageHead title="Logs" sub="What the server did lately, newest at the bottom" actions={<>
        <button type="button" className="btn" onClick={() => setPaused((p) => !p)}><Icon name={paused ? "play_arrow" : "pause"} size={18} />{paused ? "Resume" : "Pause"}</button>
        <button type="button" className="btn" onClick={() => { navigator.clipboard?.writeText(shown.join("\n")); notify("Logs copied"); }}><Icon name="content_copy" size={18} />Copy</button>
      </>} />
      <div className="page-body stack">
        <div className="bar">
          <input className="input grow" type="search" placeholder="Filter" aria-label="Filter" value={q} onChange={(e) => setQ(e.target.value)} />
          <label className="edit-switch"><Switch label="Only errors" on={errorsOnly} onChange={setErrorsOnly} />Only errors</label>
        </div>
        <div className="log tall" ref={box} role="log" onScroll={(e) => {
          const b = e.currentTarget;
          stick.current = b.scrollTop + b.clientHeight >= b.scrollHeight - 30;
        }}>
          {shown.length ? shown.map((l, i) => {
            const m = l.match(LINE);
            return (
              <div key={i} className={"log-line" + (ERROR_LINE.test(l) ? " error" : "")}>
                {m ? <><span className="log-time">{m[2]}</span>{m[3]}</> : l}
              </div>
            );
          }) : <div>{lines.length ? "No lines match." : "Nothing logged yet."}</div>}
        </div>
      </div>
    </>
  );
}
