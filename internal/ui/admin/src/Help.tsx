import { useEffect, type ReactNode } from "react";
import configuration from "../../../../docs/configuration.md?raw";
import dashboard from "../../../../docs/dashboard.md?raw";
import howItWorks from "../../../../docs/how-it-works.md?raw";
import troubleshooting from "../../../../docs/troubleshooting.md?raw";
import webmail from "../../../../docs/webmail.md?raw";
import type { IconName } from "../../shared/Icon";

// The guides in docs/, the same files the repository shows, built in. The
// sidebar lists them; #/help/<guide>/<heading> opens one at a heading.
const GUIDES: Record<string, string> = {
  "how-it-works": howItWorks,
  configuration,
  dashboard,
  webmail,
  troubleshooting,
};

export const HELP: [string, string, IconName][] = [
  ["how-it-works", "How it works", "info"],
  ["configuration", "Configuration", "settings"],
  ["dashboard", "The admin dashboard", "menu_book"],
  ["webmail", "The webmail", "mail"],
  ["troubleshooting", "When something is off", "help"],
];

const slug = (text: string) => text.toLowerCase().replace(/[^a-z0-9 -]/g, "").trim().replace(/ +/g, "-");

export function Help({ guide, heading }: { guide: string; heading: string }) {
  const [key, title] = HELP.find(([k]) => k === guide) ?? HELP[0];

  useEffect(() => {
    if (!heading) return;
    const f = requestAnimationFrame(() => document.getElementById("doc-" + heading)?.scrollIntoView());
    return () => cancelAnimationFrame(f);
  }, [key, heading]);

  return (
    <>
      <div className="page-head">
        <div className="page-head-main">
          <h1>Help</h1>
          <div className="page-sub">How Mailer works and how to use it</div>
        </div>
      </div>
      <div className="page-body stack flow" key={key}>
        <h2 className="doc-title">{title}</h2>
        <Markdown source={GUIDES[key]} />
      </div>
    </>
  );
}

// A link to another guide ("configuration.md#ports") opens it here.
function guideHref(target: string) {
  const [file, anchor] = target.split("#");
  const g = file.replace(/\.md$/, "");
  return GUIDES[g] ? `#/help/${g}${anchor ? "/" + anchor : ""}` : "#/help";
}

const INLINE = /\*\*(.+?)\*\*|`([^`]+)`|\[([^\]]+)\]\(([^)]+)\)|<(https?:\/\/[^>]+)>/g;

function inline(text: string): ReactNode[] {
  const out: ReactNode[] = [];
  let last = 0;
  for (const m of text.matchAll(INLINE)) {
    const at = m.index ?? 0;
    if (at > last) out.push(text.slice(last, at));
    const [, bold, code, label, target, auto] = m;
    if (bold) out.push(<b key={at}>{inline(bold)}</b>);
    else if (code) out.push(<code key={at}>{code}</code>);
    else if (auto) out.push(<a key={at} href={auto} target="_blank" rel="noreferrer">{auto}</a>);
    else if (/^https?:/.test(target)) out.push(<a key={at} href={target} target="_blank" rel="noreferrer">{inline(label)}</a>);
    else out.push(<a key={at} href={guideHref(target)}>{inline(label)}</a>);
    last = at + m[0].length;
  }
  if (last < text.length) out.push(text.slice(last));
  return out;
}

const cells = (row: string) => row.trim().replace(/^\||\|$/g, "").split("|").map((c) => c.trim());
const isBlock = (l: string) => /^(#{1,4} |- |\d+\. |\|)/.test(l);

// Markdown draws a guide as the help pages' sections: each ## heading opens
// one, with paragraphs, lists, tables, bold, code and links, which is all the
// guides use. The guide's own # title is the page's.
function Markdown({ source }: { source: string }) {
  const lines = source.replace(/\r/g, "").split("\n");
  const sections: { title: string; blocks: ReactNode[] }[] = [{ title: "", blocks: [] }];
  let i = 0;
  while (i < lines.length) {
    const line = lines[i];
    const blocks = sections[sections.length - 1].blocks;
    const k = blocks.length;
    const heading = /^(#{1,4}) (.*)$/.exec(line);
    if (!line.trim()) {
      i++;
    } else if (heading) {
      const [, hashes, text] = heading;
      if (hashes.length === 2) sections.push({ title: text, blocks: [] });
      if (hashes.length > 2) blocks.push(<p key={k} id={"doc-" + slug(text)}><b>{inline(text)}</b></p>);
      i++;
    } else if (line.startsWith("|")) {
      const rows: string[] = [];
      while (i < lines.length && lines[i].startsWith("|")) rows.push(lines[i++]);
      const [head, , ...body] = rows.map(cells);
      blocks.push(
        <div key={k} className="table-wrap">
          <table className="table doc-table">
            <thead><tr>{head.map((c, j) => <th key={j}>{inline(c)}</th>)}</tr></thead>
            <tbody>{body.map((r, n) => <tr key={n}>{r.map((c, j) => <td key={j}>{inline(c)}</td>)}</tr>)}</tbody>
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
      const li = items.map((t, n) => <li key={n}>{inline(t)}</li>);
      blocks.push(ordered ? <ol key={k} className="bullets">{li}</ol> : <ul key={k} className="bullets">{li}</ul>);
    } else {
      const para: string[] = [];
      while (i < lines.length && lines[i].trim() && !isBlock(lines[i])) para.push(lines[i++].trim());
      blocks.push(<p key={k}>{inline(para.join(" "))}</p>);
    }
  }
  return (
    <>
      {sections.filter((s) => s.title || s.blocks.length).map((s, n) => (
        <section key={n} className="doc" id={s.title ? "doc-" + slug(s.title) : undefined}>
          {s.title && <h3>{inline(s.title)}</h3>}
          {s.blocks}
        </section>
      ))}
    </>
  );
}
