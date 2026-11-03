import type { Addr, Detail } from "./types";

// HTML from a message, a paste or a signature is rebuilt from an inert parse
// before it goes into the editor: only these elements and attributes are
// copied, links only to the web or an address, pictures only those carried
// in the message itself. Nothing in it can run.
const ELEMENTS = new Set([
  "a", "b", "strong", "i", "em", "u", "s", "strike", "del", "ins", "sub", "sup", "small", "big", "mark", "font", "span", "div", "p",
  "br", "hr", "ul", "ol", "li", "blockquote", "pre", "code", "h1", "h2", "h3", "h4", "h5", "h6",
  "table", "thead", "tbody", "tfoot", "tr", "td", "th", "caption", "colgroup", "col", "img", "center",
]);
const DROP = new Set(["script", "style", "head", "title", "meta", "link", "iframe", "frame", "object", "embed", "template", "noscript",
  "svg", "math", "form", "input", "button", "textarea", "select", "audio", "video", "canvas", "base"]);
const ATTRS: Record<string, string[]> = {
  "*": ["style", "align", "dir", "title"],
  a: ["href"],
  img: ["src", "alt", "width", "height"],
  font: ["color", "size", "face"],
  table: ["border", "cellpadding", "cellspacing", "width", "bgcolor"],
  td: ["colspan", "rowspan", "valign", "width", "bgcolor"],
  th: ["colspan", "rowspan", "valign", "width", "bgcolor"],
  col: ["span", "width"],
  ol: ["start", "type"],
};

const safeStyle = (s: string) => (/url\s*\(|expression|behavior|@import|javascript:|-moz-binding/i.test(s) ? "" : s);

function copy(from: Node, to: Node) {
  for (const n of Array.from(from.childNodes)) {
    if (n.nodeType === Node.TEXT_NODE) {
      to.appendChild(document.createTextNode(n.textContent ?? ""));
      continue;
    }
    if (n.nodeType !== Node.ELEMENT_NODE) continue;
    const el = n as Element;
    const tag = el.tagName.toLowerCase();
    if (DROP.has(tag)) continue;
    if (!ELEMENTS.has(tag)) {
      copy(el, to);
      continue;
    }
    const out = document.createElement(tag);
    for (const attr of Array.from(el.attributes)) {
      const name = attr.name.toLowerCase();
      if (!ATTRS["*"].includes(name) && !ATTRS[tag]?.includes(name)) continue;
      let value = attr.value.trim();
      if (name === "href" && !/^(https?:|mailto:)/i.test(value)) continue;
      if (name === "src" && !/^data:image\/(png|gif|jpe?g|webp|bmp);base64,/i.test(value)) continue;
      if (name === "style") value = safeStyle(value);
      if (value === "" && name !== "alt") continue;
      out.setAttribute(name, value);
    }
    if (tag === "a") {
      out.setAttribute("target", "_blank");
      out.setAttribute("rel", "noopener noreferrer");
    }
    copy(el, out);
    to.appendChild(out);
  }
}

export function clean(html: string): string {
  const doc = new DOMParser().parseFromString(html, "text/html");
  const box = document.createElement("div");
  copy(doc.body, box);
  return box.innerHTML;
}

export function escape(s: string): string {
  return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
}

// textToHTML is plain text as editor HTML, line breaks kept.
export const textToHTML = (s: string) => escape(s).replace(/\r?\n/g, "<br>");

export const who = (a: Addr) => a.name || a.email;

export const addrText = (a: Addr) => (a.name ? `${a.name} <${a.email}>` : a.email);

export const addrList = (list: Addr[] | null | undefined) => (list ?? []).map(addrText).join(", ");

export function longDate(s: string) {
  const d = new Date(s);
  return isNaN(d.getTime()) ? "" : d.toLocaleString([], { weekday: "short", year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" });
}

// listDate is the time today, the day and month this year, the date before.
export function listDate(s: string) {
  const d = new Date(s);
  if (isNaN(d.getTime())) return "";
  const now = new Date();
  if (d.toDateString() === now.toDateString()) return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  if (d.getFullYear() === now.getFullYear()) return d.toLocaleDateString([], { month: "short", day: "numeric" });
  return d.toLocaleDateString([], { year: "numeric", month: "short", day: "numeric" });
}

// quoted is the earlier message under a reply or a forward: a line saying
// who wrote it and when, then its text, cleaned.
export function quoted(d: Detail, forward: boolean) {
  const body = d.html && d.source ? clean(d.source) : textToHTML(d.text);
  const head = forward
    ? `<div>---------- Forwarded message ----------<br>From: ${escape(addrText(d.from))}<br>Date: ${escape(longDate(d.date))}<br>` +
      `Subject: ${escape(d.subject)}<br>To: ${escape(addrList(d.to))}${d.cc?.length ? "<br>Cc: " + escape(addrList(d.cc)) : ""}</div><br>`
    : `<div>On ${escape(longDate(d.date))}, ${escape(addrText(d.from))} wrote:</div>`;
  return forward ? head + body : head + `<blockquote style="margin:0 0 0 .8ex;border-left:2px solid #c4c7c5;padding-left:1ex">${body}</blockquote>`;
}

// signatureHTML is a signature as it goes under a message.
export const signatureHTML = (html: string) => `<div data-signature="1">${clean(html)}</div>`;

// subjectFor adds Re: or Fwd: once.
export function subjectFor(subject: string, forward: boolean) {
  if (forward) return /^fwd?\s*:/i.test(subject) ? subject : "Fwd: " + subject;
  return /^re\s*:/i.test(subject) ? subject : "Re: " + subject;
}

export function initials(a: Addr) {
  const name = a.name || a.email;
  return name.split(/[\s._@-]+/).filter(Boolean).slice(0, 2).map((w) => w[0]!.toUpperCase()).join("") || "?";
}

export function hue(s: string) {
  let h = 0;
  for (const c of s) h = (h * 31 + c.charCodeAt(0)) % 360;
  return h;
}

export const URL_RE = /\b(https?:\/\/[^\s<>"']+[^\s<>"'.,;:!?)\]])/g;
