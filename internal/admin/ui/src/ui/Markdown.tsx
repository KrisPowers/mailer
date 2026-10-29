import type { ReactNode } from "react";

// Markdown draws the guides in docs/: headings, paragraphs, lists, tables,
// bold, code and links, which is all they use. guide turns a link to
// another guide ("configuration.md#ports") into a dashboard address.

const headingId = (text: string) =>
  "doc-" + text.toLowerCase().replace(/[^a-z0-9 -]/g, "").trim().replace(/ +/g, "-");

const INLINE = /\*\*(.+?)\*\*|`([^`]+)`|\[([^\]]+)\]\(([^)]+)\)|<(https?:\/\/[^>]+)>/g;

function inline(text: string, guide: (target: string) => string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(INLINE)) {
    if (m.index > last) out.push(text.slice(last, m.index));
    const [, bold, code, label, target, auto] = m;
    const k = m.index;
    if (bold) out.push(<b key={k}>{inline(bold, guide)}</b>);
    else if (code) out.push(<code key={k}>{code}</code>);
    else if (auto) out.push(<a key={k} href={auto} target="_blank" rel="noreferrer">{auto}</a>);
    else if (/^https?:/.test(target)) out.push(<a key={k} href={target} target="_blank" rel="noreferrer">{inline(label, guide)}</a>);
    else out.push(<a key={k} href={guide(target)}>{inline(label, guide)}</a>);
    last = k + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

const cells = (row: string) => row.trim().replace(/^\||\|$/g, "").split("|").map((c) => c.trim());
const isBlock = (l: string) => /^(#{1,4} |- |\d+\. |\|)/.test(l);

export function Markdown({ source, guide }: { source: string; guide: (target: string) => string }) {
  const lines = source.replace(/\r/g, "").split("\n");
  const blocks: ReactNode[] = [];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const k = blocks.length;
    const heading = /^(#{1,4}) (.*)$/.exec(line);
    if (!line.trim()) {
      i++;
    } else if (heading) {
      // The page's own title is the tab it's under.
      const [, hashes, text] = heading;
      if (hashes.length === 2) blocks.push(<h2 key={k} id={headingId(text)}>{inline(text, guide)}</h2>);
      if (hashes.length > 2) blocks.push(<h3 key={k} id={headingId(text)}>{inline(text, guide)}</h3>);
      i++;
    } else if (line.startsWith("|")) {
      const rows: string[] = [];
      while (i < lines.length && lines[i].startsWith("|")) rows.push(lines[i++]);
      const [head, , ...body] = rows.map(cells);
      blocks.push(
        <div key={k} className="table-wrap">
          <table className="table">
            <thead><tr>{head.map((c, j) => <th key={j} className={j === head.length - 1 ? "grow" : undefined}>{inline(c, guide)}</th>)}</tr></thead>
            <tbody>{body.map((r, n) => <tr key={n}>{r.map((c, j) => <td key={j} className="wrap">{inline(c, guide)}</td>)}</tr>)}</tbody>
          </table>
        </div>,
      );
    } else if (/^(- |\d+\. )/.test(line)) {
      const ordered = /^\d/.test(line);
      const items: string[] = [];
      while (i < lines.length && (/^(- |\d+\. )/.test(lines[i]) || (lines[i].startsWith("  ") && items.length))) {
        const l = lines[i++];
        if (l.startsWith("  ")) items[items.length - 1] += " " + l.trim();
        else items.push(l.replace(/^(- |\d+\. )/, ""));
      }
      const li = items.map((t, n) => <li key={n}>{inline(t, guide)}</li>);
      blocks.push(ordered ? <ol key={k}>{li}</ol> : <ul key={k}>{li}</ul>);
    } else {
      const para: string[] = [];
      while (i < lines.length && lines[i].trim() && !isBlock(lines[i])) para.push(lines[i++].trim());
      blocks.push(<p key={k}>{inline(para.join(" "), guide)}</p>);
    }
  }
  return <article className="doc">{blocks}</article>;
}
