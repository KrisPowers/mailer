"use strict";

// ---------- helpers ----------

const $ = (id) => document.getElementById(id);
const SVGNS = "http://www.w3.org/2000/svg";

function h(tag, attrs = {}, ...children) {
  const e = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k === "class") e.className = v;
    else if (k.startsWith("on")) e.addEventListener(k.slice(2), v);
    else e.setAttribute(k, v === true ? "" : v);
  }
  for (const c of children.flat()) {
    if (c === undefined || c === null || c === false) continue;
    e.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return e;
}

function icon(name, cls = "i") {
  const s = document.createElementNS(SVGNS, "svg");
  s.setAttribute("class", cls);
  const u = document.createElementNS(SVGNS, "use");
  u.setAttribute("href", "#i-" + name);
  s.append(u);
  return s;
}

async function api(method, path, body) {
  const opts = { method, headers: { "X-Mailer": "1" } };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const r = await fetch(path, opts);
  const data = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(data.error || r.statusText);
  return data;
}

function toast(msg, bad = false) {
  const t = h("div", { class: "toast" + (bad ? " bad" : "") }, icon(bad ? "alert" : "check"), h("span", {}, msg));
  $("toasts").append(t);
  setTimeout(() => t.remove(), bad ? 6000 : 2800);
}

async function copy(text, what = "Copied") {
  try {
    await navigator.clipboard.writeText(text);
    toast(what);
  } catch {
    toast("Couldn't copy — select and copy manually", true);
  }
}

function copyBtn(text, label) {
  return h("button", { class: "icon-btn", title: "Copy", onclick: (e) => { e.stopPropagation(); copy(text, label); } }, icon("copy"));
}

function fmtSize(n) {
  if (!n) return "0 B";
  if (n < 1024) return n + " B";
  if (n < 1 << 20) return (n / 1024).toFixed(0) + " KB";
  if (n < 1 << 30) return (n / (1 << 20)).toFixed(1) + " MB";
  return (n / (1 << 30)).toFixed(2) + " GB";
}

function validDate(s) {
  const d = new Date(s);
  return isNaN(d) || d.getFullYear() < 2000 ? null : d;
}

function fmtTime(s) {
  const d = validDate(s);
  return d ? d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "numeric", minute: "2-digit" }) : "—";
}

function timeAgo(s) {
  const d = validDate(s);
  if (!d) return "never";
  const sec = Math.round((Date.now() - d) / 1000);
  if (sec < 60) return "just now";
  if (sec < 3600) return Math.floor(sec / 60) + "m ago";
  if (sec < 86400) return Math.floor(sec / 3600) + "h ago";
  return Math.floor(sec / 86400) + "d ago";
}

function badge(kind, text) {
  return h("span", { class: "badge " + kind }, text);
}

async function withBusy(btn, fn) {
  if (!btn) return fn();
  const old = btn.innerHTML;
  btn.disabled = true;
  btn.textContent = "Working…";
  try {
    return await fn();
  } finally {
    btn.disabled = false;
    btn.innerHTML = old;
  }
}

function store(key, val) {
  try {
    if (val === undefined) return localStorage.getItem(key);
    localStorage.setItem(key, val);
  } catch { /* storage unavailable */ }
  return null;
}

// ---------- floating: popover & menu ----------

let floatAnchor = null;

function closeFloating() {
  $("popover").hidden = true;
  $("menu").hidden = true;
  floatAnchor?.classList.remove("open");
  floatAnchor = null;
}

function place(elm, anchor) {
  elm.hidden = false;
  const r = anchor.getBoundingClientRect();
  const w = elm.offsetWidth, ht = elm.offsetHeight;
  let left = Math.min(r.right - w, window.innerWidth - w - 8);
  left = Math.max(8, left);
  let top = r.bottom + 6;
  if (top + ht > window.innerHeight - 8) top = Math.max(8, r.top - ht - 6);
  elm.style.left = left + "px";
  elm.style.top = top + "px";
}

function toggleFloating(elm, anchor, build) {
  const same = floatAnchor === anchor && !elm.hidden;
  closeFloating();
  if (same) return;
  elm.replaceChildren(...build());
  floatAnchor = anchor;
  anchor.classList.add("open");
  place(elm, anchor);
}

function showMenu(anchor, items) {
  toggleFloating($("menu"), anchor, () => items.map((it) => it === "-" ? h("hr") :
    h("button", { class: it.danger ? "danger" : "", onclick: () => { closeFloating(); it.onClick(); } },
      icon(it.icon), it.label)));
}

function showPopover(anchor, build) {
  toggleFloating($("popover"), anchor, build);
}

function moreBtn(items) {
  return h("button", { class: "icon-btn", title: "Actions", onclick: (e) => { e.stopPropagation(); showMenu(e.currentTarget, items()); } }, icon("more"));
}

document.addEventListener("click", (e) => {
  if (!e.target.closest(".popover, .menu, .status-btn, #btn-client")) closeFloating();
});
document.addEventListener("keydown", (e) => { if (e.key === "Escape") closeFloating(); });
window.addEventListener("resize", closeFloating);
$("view").parentElement.addEventListener("scroll", closeFloating);

// ---------- modal ----------

function modal({ title, body, actions = [], wide = false, onClose }) {
  const dlg = $("modal");
  dlg.className = wide ? "wide" : "";
  const close = () => { dlg.close(); };
  const foot = h("div", { class: "modal-foot" });
  const setActions = (acts) => foot.replaceChildren(...acts.map((a) =>
    h("button", { class: "btn" + (a.primary ? " primary" : "") + (a.danger ? " danger" : ""),
      onclick: async (e) => {
        if (!a.onClick) return close();
        try { await withBusy(e.currentTarget, () => a.onClick(close)); } catch (err) { toast(err.message, true); }
      } }, a.icon ? icon(a.icon) : null, a.label)));
  setActions(actions);
  const bodyEl = h("div", { class: "modal-body" }, body);
  dlg.replaceChildren(
    h("div", { class: "modal-head" }, h("h2", {}, title),
      h("button", { class: "icon-btn", title: "Close", onclick: close }, icon("x"))),
    bodyEl, foot);
  dlg.onclose = () => onClose?.();
  if (!dlg.open) dlg.showModal();
  return {
    close,
    setBody: (b) => bodyEl.replaceChildren(b),
    setActions,
    setTitle: (t) => (dlg.querySelector(".modal-head h2").textContent = t),
  };
}

function confirmModal(title, message, label, danger, onYes) {
  modal({ title, body: h("p", {}, message), actions: [
    { label: "Cancel" },
    { label, danger, primary: !danger, onClick: async (close) => { close(); await onYes(); } },
  ] });
}

// ---------- state & top bar ----------

let ov = null;

function statusOf() {
  const c = ov?.checks;
  const out = { cf: "none", ts: "none", out: "none" };
  if (!c) return out;
  const cf = c.cloudflare;
  if (!cf.connected) out.cf = "bad";
  else out.cf = cf.tunnel_status === "healthy" && cf.email_routing === "ready" && cf.catch_all_ok && cf.public_healthy ? "ok" : "warn";
  const ts = c.tailscale;
  if (!ts.installed || !ts.running) out.ts = "bad";
  else out.ts = ts.funnel_imap && ts.funnel_submission ? "ok" : "warn";
  const p25 = c.items.find((i) => i.name.startsWith("Outbound port 25"));
  out.out = p25 ? (p25.ok ? (ov.queue ? "warn" : "ok") : "bad") : "none";
  return out;
}

function setupStep() {
  if (!ov) return 0;
  if (!ov.cloudflare_setup) return 1;
  if (!ov.tailscale_setup) return 2;
  if (!ov.accounts) return 3;
  return 0;
}

async function loadOverview() {
  ov = await api("GET", "/api/overview");
  $("top-domain").textContent = ov.domain || "not set up";
  $("nav-accounts").textContent = ov.accounts || "";
  $("nav-queue").textContent = ov.queue || "";
  const st = statusOf();
  for (const [id, key] of [["st-cf", "cf"], ["st-ts", "ts"], ["st-out", "out"]]) {
    $(id).classList.remove("ok", "bad", "warn");
    if (st[key] !== "none") $(id).classList.add(st[key]);
  }
  const step = setupStep();
  $("setup-banner").hidden = step === 0;
  $("setup-banner-text").textContent = [
    "", "Setup step 1 of 3: connect Cloudflare so mail can reach this server.",
    "Setup step 2 of 3: enable Tailscale so Outlook and Gmail can connect.",
    "Setup step 3 of 3: create your first mail account.",
  ][step];
  return ov;
}

function line(label, value) {
  return h("div", { class: "row-line" }, h("span", {}, label), h("span", {}, value));
}

const kindText = { ok: "Healthy", warn: "Attention", bad: "Problem", none: "Unknown" };

$("st-cf").onclick = (e) => showPopover(e.currentTarget, () => {
  const cf = ov?.checks?.cloudflare || {};
  const tun = ov?.checks?.tunnel || {};
  const k = statusOf().cf;
  return [
    h("div", { class: "pop-head" }, icon("cloud"), "Cloudflare", badge(k, cf.connected ? kindText[k] : "Not connected")),
    h("div", { class: "pop-body" }, cf.connected ? [
      line("Domain", cf.domain),
      line("Tunnel", (cf.tunnel_status || "—") + ` · ${cf.tunnel_connections || 0} conn`),
      line("Connector (cloudflared)", tun.running ? "running" : (tun.last_error || "not running")),
      line("Email Routing", cf.email_routing || "—"),
      line("Catch-all → Worker", cf.catch_all_ok ? "yes" : "no"),
      line("Inbound path", cf.public_healthy ? "reachable" : "unreachable"),
      line("Connected", fmtTime(ov.connected_at)),
    ] : h("p", { class: "muted" }, "Cloudflare receives your mail and passes it to this server through a tunnel.")),
    h("div", { class: "pop-foot" }, cf.connected
      ? h("button", { class: "btn sm", onclick: () => { closeFloating(); repairCloudflare(); } }, icon("refresh"), "Repair")
      : h("button", { class: "btn sm primary", onclick: () => { closeFloating(); openWizard(); } }, "Set up")),
  ];
});

$("st-ts").onclick = (e) => showPopover(e.currentTarget, () => {
  const ts = ov?.checks?.tailscale || {};
  const k = statusOf().ts;
  return [
    h("div", { class: "pop-head" }, icon("globe"), "Tailscale", badge(k, kindText[k])),
    h("div", { class: "pop-body" },
      line("Installed", ts.installed ? "yes" : "no"),
      line("Signed in", ts.running ? "yes" : (ts.state || "no")),
      ts.dns_name ? line("Address", ts.dns_name) : null,
      line("Funnel · IMAP :443", ts.funnel_imap ? "on" : "off"),
      line("Funnel · SMTP :8443", ts.funnel_submission ? "on" : "off")),
    h("div", { class: "pop-foot" },
      !ts.installed ? h("a", { class: "btn sm", href: "https://tailscale.com/download", target: "_blank", rel: "noopener" }, icon("external"), "Download")
        : h("button", { class: "btn sm", onclick: () => { closeFloating(); enableTailscale(); } }, icon("refresh"), ts.funnel_imap ? "Re-apply" : "Enable")),
  ];
});

$("st-out").onclick = (e) => showPopover(e.currentTarget, () => {
  const p25 = ov?.checks?.items.find((i) => i.name.startsWith("Outbound port 25"));
  const k = statusOf().out;
  return [
    h("div", { class: "pop-head" }, icon("send"), "Mail flow", badge(k, kindText[k])),
    h("div", { class: "pop-body" },
      line("Outbound port 25", p25 ? (p25.ok ? "open" : "blocked") : "—"),
      line("Waiting to send", String(ov?.queue ?? 0)),
      line("Last received", timeAgo(ov?.last_inbound))),
    h("div", { class: "pop-foot" }, h("a", { class: "btn sm", href: "#queue", onclick: closeFloating }, "View queue")),
  ];
});

function clientKV() {
  if (!ov?.client) return h("p", { class: "muted" }, "Available once Tailscale is enabled.");
  const c = ov.client;
  return h("dl", { class: "kv" },
    h("dt", {}, "Account type"), h("dd", {}, "IMAP"), h("span"),
    h("dt", {}, "Incoming server"), h("dd", { class: "mono" }, c.server), copyBtn(c.server, "Server copied"),
    h("dt", {}, "Incoming port"), h("dd", {}, `${c.imap_port} · SSL/TLS`), h("span"),
    h("dt", {}, "Outgoing server"), h("dd", { class: "mono" }, c.server), copyBtn(c.server, "Server copied"),
    h("dt", {}, "Outgoing port"), h("dd", {}, `${c.smtp_port} · SSL/TLS`), h("span"),
    h("dt", {}, "Username"), h("dd", {}, "full email address"), h("span"));
}

$("btn-client").onclick = (e) => showPopover(e.currentTarget, () => [
  h("div", { class: "pop-head" }, icon("phone"), "Outlook / Gmail app"),
  h("div", { class: "pop-body" }, clientKV()),
]);

$("btn-refresh").onclick = async (e) => {
  const b = e.currentTarget;
  b.disabled = true;
  try {
    await api("POST", "/api/check");
    await loadOverview();
    render();
    toast("Connections re-checked");
  } catch (err) { toast(err.message, true); } finally { b.disabled = false; }
};

// sidebar
if (store("mailer.collapsed") === "1") $("shell").classList.add("collapsed");
$("btn-collapse").onclick = () => {
  const c = $("shell").classList.toggle("collapsed");
  store("mailer.collapsed", c ? "1" : "0");
};
$("btn-nav").onclick = () => $("shell").classList.toggle("nav-open");
$("btn-setup").onclick = () => openWizard();

// ---------- setup wizard ----------

function progressList(steps, error) {
  return h("ul", { class: "progress" },
    (steps || []).map((s) => h("li", { class: s.ok ? "ok" : "err" }, icon(s.ok ? "check" : "x"), s.name, h("span", { class: "d" }, s.detail || ""))),
    error ? h("li", { class: "err" }, icon("alert"), error) : null);
}

function wizardSteps(current) {
  const names = ["Cloudflare", "Tailscale", "First account"];
  return h("div", { class: "wiz-steps" }, names.map((n, i) => {
    const num = i + 1;
    const done = (num === 1 && ov.cloudflare_setup) || (num === 2 && ov.tailscale_setup) || (num === 3 && ov.accounts > 0);
    return h("div", { class: "wiz-step" + (num === current ? " current" : "") + (done ? " done" : "") },
      h("span", { class: "n" }, done ? icon("check") : String(num)), n);
  }));
}

let wiz = null;

function openWizard(forceStep) {
  const step = forceStep || setupStep();
  if (!step) {
    wiz?.close();
    toast("Setup complete");
    return;
  }
  const builders = { 1: wizCloudflare, 2: wizTailscale, 3: wizAccount };
  const { body, actions } = builders[step]();
  const content = h("div", {}, wizardSteps(step), body);
  if (wiz && $("modal").open) {
    wiz.setBody(content);
    wiz.setActions(actions);
  } else {
    wiz = modal({ title: "Set up Mailer", body: content, actions, wide: true, onClose: () => { wiz = null; } });
  }
}

async function advance() {
  await loadOverview();
  render();
  openWizard();
}

function wizCloudflare() {
  const token = h("input", { type: "password", placeholder: "Paste Cloudflare API token", autocomplete: "off", spellcheck: "false", style: "width:100%" });
  const zoneSel = h("select", { hidden: true, style: "width:100%" });
  const perms = h("ul", { class: "perm-list", hidden: true },
    ["Account · Cloudflare Tunnel · Edit", "Account · Workers Scripts · Edit", "Zone · Zone · Read",
      "Zone · Zone Settings · Edit", "Zone · DNS · Edit", "Zone · Email Routing Rules · Edit"].map((p) => h("li", {}, p)));
  const out = h("div");
  const body = h("div", {},
    h("p", {}, "Cloudflare receives mail for your domain and passes it to this server through a tunnel. One token sets up everything."),
    h("div", { class: "field" },
      h("label", {}, "API token"), token,
      h("div", { class: "hint" },
        h("a", { href: "https://dash.cloudflare.com/profile/api-tokens", target: "_blank", rel: "noopener" }, "Create a custom token"),
        " · ",
        h("button", { class: "info-btn", onclick: () => (perms.hidden = !perms.hidden) }, icon("info"), "Required permissions"),
        perms)),
    h("div", { class: "field" }, zoneSel),
    out);
  const run = async () => {
    out.replaceChildren(h("p", { class: "muted" }, "Configuring Cloudflare… this can take a minute."));
    const r = await api("POST", "/api/cloudflare/connect", { token: token.value.trim(), zone: zoneSel.hidden ? "" : zoneSel.value });
    if (r.choose_zone) {
      zoneSel.replaceChildren(...r.choose_zone.map((z) => h("option", { value: z }, z)));
      zoneSel.hidden = false;
      out.replaceChildren(h("p", {}, "This token can see several domains. Choose one and click Connect again."));
      return;
    }
    out.replaceChildren(progressList(r.steps, r.error));
    if (!r.error) {
      toast("Cloudflare connected");
      setTimeout(advance, 900);
    }
  };
  return { body, actions: [{ label: "Close" }, { label: "Connect", primary: true, onClick: run }] };
}

function wizTailscale() {
  const out = h("div");
  const body = h("div", {},
    h("p", {}, "Tailscale Funnel lets Outlook and the Gmail app reach this server securely, with nothing opened on your router."),
    h("ol", { class: "perm-list" },
      h("li", {}, "Install Tailscale on this machine and sign in ", h("a", { href: "https://tailscale.com/download", target: "_blank", rel: "noopener" }, "(download)"), "."),
      h("li", {}, "Click Enable. The first time, Tailscale may ask you to allow Funnel with one click.")),
    out);
  const run = async () => {
    const r = await enableTailscale(true);
    if (r?.ok) {
      out.replaceChildren(h("p", {}, icon("check"), " Enabled: ", h("code", {}, r.dns_name)));
      setTimeout(advance, 900);
      return;
    }
    out.replaceChildren(h("div", { class: "note" }, icon("alert"), h("div", {},
      h("div", {}, r?.error || "Couldn't enable Funnel."),
      r?.action_url ? h("a", { href: r.action_url, target: "_blank", rel: "noopener" }, "Open Tailscale ", icon("external")) : null,
      r?.action_url ? h("div", { class: "muted" }, "After approving, click Enable again.") : null)));
  };
  return { body, actions: [{ label: "Close" }, { label: "Enable", primary: true, onClick: run }] };
}

function wizAccount() {
  const input = h("input", { type: "text", placeholder: "you", spellcheck: "false" });
  const body = h("div", {},
    h("p", {}, "Create the mailbox you'll use in Outlook or the Gmail app."),
    h("div", { class: "field" }, h("label", {}, "Address"),
      h("div", { class: "input-suffix" }, input, h("span", {}, "@" + ov.domain))));
  setTimeout(() => input.focus(), 50);
  return { body, actions: [{ label: "Close" }, { label: "Create account", primary: true, onClick: async (close) => {
    const r = await api("POST", "/api/accounts", { email: input.value });
    await loadOverview();
    render();
    showPassword(r.email, r.password, "Account created");
  } }] };
}

async function enableTailscale(quiet) {
  try {
    const r = await api("POST", "/api/tailscale/enable");
    await loadOverview();
    render();
    if (!quiet) {
      if (r.ok) toast("Funnel enabled on " + r.dns_name);
      else if (r.action_url) {
        modal({ title: "Tailscale needs approval", body: h("div", {},
          h("p", {}, r.error),
          h("a", { class: "btn primary", href: r.action_url, target: "_blank", rel: "noopener" }, icon("external"), "Open Tailscale")),
        actions: [{ label: "Close" }, { label: "Try again", onClick: async (close) => { close(); await enableTailscale(); } }] });
      } else toast(r.error, true);
    }
    return r;
  } catch (err) {
    if (!quiet) toast(err.message, true);
    return { error: err.message };
  }
}

async function repairCloudflare() {
  const m = modal({ title: "Repair Cloudflare setup", body: h("p", { class: "muted" }, "Re-checking and fixing tunnel, Worker, Email Routing and DNS…"), actions: [{ label: "Close" }] });
  try {
    const r = await api("POST", "/api/cloudflare/connect", { token: "", zone: ov.domain });
    m.setBody(h("div", {}, progressList(r.steps, r.error)));
    if (!r.error) toast("Cloudflare is configured");
    await loadOverview();
    render();
  } catch (err) {
    m.setBody(h("p", { class: "muted" }, err.message));
  }
}

function showPassword(email, pw, title) {
  modal({ title, body: h("div", {},
    h("p", {}, "Password for ", h("b", {}, email), ":"),
    h("div", { class: "secret-box" }, h("span", {}, pw), copyBtn(pw, "Password copied")),
    h("div", { class: "note" }, icon("alert"), h("span", {}, "This is the only time it's shown. Save it in your mail app now.")),
    h("h3", { class: "small muted", style: "margin:16px 0 8px;text-transform:uppercase;letter-spacing:.04em" }, "Client settings"),
    clientKV()),
  actions: [{ label: "Done", primary: true, onClick: async (close) => { close(); if (setupStep()) openWizard(); } }], wide: true });
}

// ---------- views ----------

const views = {};
let current = "dashboard";
let logTimer = null;

function render() {
  const name = location.hash.slice(1) || "dashboard";
  current = views[name] ? name : "dashboard";
  document.querySelectorAll(".sidebar a").forEach((a) => a.classList.toggle("active", a.dataset.view === current));
  $("shell").classList.remove("nav-open");
  clearTimeout(logTimer);
  closeFloating();
  views[current]().catch((err) => {
    $("view").replaceChildren(h("div", { class: "panel" }, h("div", { class: "empty" }, err.message)));
  });
}
window.addEventListener("hashchange", render);

function pageHead(title, sub, ...actions) {
  return h("div", { class: "page-head" }, h("h1", {}, title), sub ? h("span", { class: "sub" }, sub) : null,
    h("div", { class: "actions" }, actions));
}

function panel(title, iconName, body, ...actions) {
  return h("div", { class: "panel" },
    title ? h("div", { class: "panel-head" }, iconName ? icon(iconName) : null, title, h("div", { class: "actions" }, actions)) : null,
    body);
}

function kpi(iconName, value, label) {
  return h("div", { class: "kpi" }, h("div", { class: "ico" }, icon(iconName)),
    h("div", {}, h("div", { class: "val" }, value), h("div", { class: "lbl" }, label)));
}

// Dashboard
views.dashboard = async () => {
  const [accounts, logs] = await Promise.all([api("GET", "/api/accounts"), api("GET", "/api/logs")]);
  const totals = accounts.reduce((t, a) => ({ msgs: t.msgs + a.messages, unseen: t.unseen + a.unseen, size: t.size + a.size }), { msgs: 0, unseen: 0, size: 0 });
  const items = ov?.checks?.items || [];

  const statusRows = items.map((i) => h("tr", {},
    h("td", {}, icon(i.ok ? "check" : "alert")),
    h("td", { class: "wrap" }, i.name, h("div", { class: "sub-detail" }, i.detail)),
    h("td", {}, badge(i.ok ? "ok" : "bad", i.ok ? "OK" : "Problem")),
    h("td", { class: "detail" }, i.detail)));

  const recent = logs.slice(-10).reverse().map((l) => {
    const m = l.match(/^(\d{4}\/\d\d\/\d\d) (\d\d:\d\d:\d\d) (.*)$/);
    return h("li", {}, h("time", {}, m ? m[2] : ""), h("span", {}, m ? m[3] : l));
  });

  $("view").replaceChildren(
    pageHead("Dashboard", ov?.domain || ""),
    h("div", { class: "kpis" },
      kpi("users", accounts.length, "Accounts"),
      kpi("inbox", totals.msgs.toLocaleString(), "Messages · " + fmtSize(totals.size)),
      kpi("mail", totals.unseen.toLocaleString(), "Unread in inboxes"),
      kpi("send", ov?.queue ?? 0, "Waiting to send"),
      kpi("clock", timeAgo(ov?.last_inbound), "Last received")),
    h("div", { class: "grid-2" },
      h("div", {},
        panel("System status", "activity",
          h("div", { class: "table-wrap" }, h("table", { class: "status-table" }, h("tbody", {}, statusRows.length ? statusRows : h("tr", {}, h("td", { colspan: 4, class: "empty" }, "Checking…"))))),
          h("span", { class: "muted small" }, ov?.checks ? "Checked " + timeAgo(ov.checks.at) : ""))),
      h("div", {},
        panel("Client settings", "phone", h("div", { class: "panel-body" }, clientKV())),
        panel("Recent activity", "terminal",
          recent.length ? h("ul", { class: "activity" }, recent) : h("div", { class: "empty" }, "No activity yet."),
          h("a", { class: "btn sm", href: "#logs" }, "All logs")))));
};

// Accounts
function accountMenu(a) {
  return () => [
    { label: "View folders", icon: "folder", onClick: () => accountDetails(a) },
    { label: "Client settings", icon: "phone", onClick: () => modal({ title: "Client settings", body: clientKV(), actions: [{ label: "Close" }] }) },
    { label: "Copy address", icon: "copy", onClick: () => copy(a.email, "Address copied") },
    "-",
    { label: "New password", icon: "key", onClick: () => confirmModal("New password", `Generate a new password for ${a.email}? The current one stops working immediately.`, "Generate", false,
      async () => { const r = await api("POST", "/api/accounts/password", { email: a.email }); showPassword(r.email, r.password, "New password"); }) },
    { label: "Delete account", icon: "trash", danger: true, onClick: () => confirmModal("Delete account", `Delete ${a.email}? It can no longer sign in. Stored mail stays on disk.`, "Delete", true,
      async () => { await api("POST", "/api/accounts/delete", { email: a.email }); toast("Deleted " + a.email); await loadOverview(); render(); }) },
  ];
}

function accountDetails(a) {
  const rows = (a.mailboxes || []).map((m) => h("tr", {},
    h("td", {}, h("span", { class: "user-cell" }, icon("folder"), m.name)),
    h("td", { class: "num" }, m.messages.toLocaleString()),
    h("td", { class: "num" }, m.unseen.toLocaleString()),
    h("td", { class: "num" }, fmtSize(m.size))));
  modal({ title: a.email, wide: true, body: h("div", {},
    h("div", { class: "panel" }, h("div", { class: "table-wrap" }, h("table", {},
      h("thead", {}, h("tr", {}, h("th", {}, "Folder"), h("th", { class: "num" }, "Messages"), h("th", { class: "num" }, "Unread"), h("th", { class: "num" }, "Size"))),
      h("tbody", {}, rows)))),
    h("p", { class: "muted small" }, a.aliases.length ? "Aliases: " + a.aliases.join(", ") : "No aliases. Add them on the Aliases page.")),
  actions: [{ label: "Close" }] });
}

function newAccountModal() {
  const input = h("input", { type: "text", placeholder: "name", spellcheck: "false" });
  modal({ title: "New account", body: h("div", {},
    h("div", { class: "field" }, h("label", {}, "Address"),
      h("div", { class: "input-suffix" }, input, h("span", {}, "@" + (ov?.domain || "…"))),
      h("div", { class: "hint" }, "A strong password is generated and shown once."))),
  actions: [{ label: "Cancel" }, { label: "Create", primary: true, onClick: async () => {
    const r = await api("POST", "/api/accounts", { email: input.value });
    await loadOverview();
    render();
    showPassword(r.email, r.password, "Account created");
  } }] });
  setTimeout(() => input.focus(), 50);
}

views.accounts = async () => {
  const accounts = await api("GET", "/api/accounts");
  const search = h("input", { type: "search", placeholder: "Search accounts" });
  const tbody = h("tbody");
  const fill = () => {
    const q = search.value.toLowerCase();
    const list = accounts.filter((a) => a.email.includes(q) || a.aliases.some((x) => x.includes(q)));
    tbody.replaceChildren(...list.map((a) => h("tr", { style: "cursor:pointer", onclick: () => accountDetails(a) },
      h("td", {}, h("div", { class: "user-cell" }, h("span", { class: "avatar" }, a.email[0]), a.email)),
      h("td", {}, a.aliases.length ? badge("plain", a.aliases.length + " alias" + (a.aliases.length > 1 ? "es" : "")) : h("span", { class: "muted" }, "—")),
      h("td", { class: "num" }, a.messages.toLocaleString()),
      h("td", { class: "num" }, a.unseen ? h("b", {}, a.unseen.toLocaleString()) : "0"),
      h("td", { class: "num" }, fmtSize(a.size)),
      h("td", { class: "act" }, moreBtn(accountMenu(a))))));
    if (!list.length) tbody.replaceChildren(h("tr", {}, h("td", { colspan: 6, class: "empty" }, accounts.length ? "No matches." : "No accounts yet.")));
  };
  search.oninput = fill;
  fill();
  $("view").replaceChildren(
    pageHead("Accounts", accounts.length + " total",
      h("div", { class: "search" }, icon("search"), search),
      h("button", { class: "btn primary", onclick: newAccountModal, disabled: !ov?.domain }, icon("plus"), "New account")),
    panel(null, null, h("div", { class: "table-wrap" }, h("table", {},
      h("thead", {}, h("tr", {}, h("th", {}, "Account"), h("th", {}, "Aliases"), h("th", { class: "num" }, "Messages"),
        h("th", { class: "num" }, "Unread"), h("th", { class: "num" }, "Size"), h("th"))),
      tbody))));
};

// Aliases
views.aliases = async () => {
  const [aliases, accounts] = await Promise.all([api("GET", "/api/aliases"), api("GET", "/api/accounts")]);
  const emails = accounts.map((a) => a.email);
  const catchKey = "@" + ov?.domain;

  const save = async (next) => {
    await api("PUT", "/api/aliases", next);
    toast("Aliases saved");
    render();
  };

  const editModal = (alias = "", target = "") => {
    const local = h("input", { type: "text", value: alias.split("@")[0], placeholder: "hello", spellcheck: "false" });
    const sel = h("select", { style: "width:100%" }, emails.map((e) => h("option", { value: e, selected: e === target }, e)));
    modal({ title: alias ? "Edit alias" : "Add alias", body: h("div", {},
      h("div", { class: "field" }, h("label", {}, "Alias"), h("div", { class: "input-suffix" }, local, h("span", {}, "@" + ov.domain))),
      h("div", { class: "field" }, h("label", {}, "Delivers to"), sel)),
    actions: [{ label: "Cancel" }, { label: "Save", primary: true, onClick: async (close) => {
      const next = { ...aliases };
      if (alias) delete next[alias];
      next[local.value.trim().toLowerCase() + "@" + ov.domain] = sel.value;
      await save(next);
      close();
    } }] });
    setTimeout(() => local.focus(), 50);
  };

  const catchOn = h("input", { type: "checkbox", checked: !!aliases[catchKey] });
  const catchSel = h("select", { disabled: !emails.length }, emails.map((e) => h("option", { value: e, selected: aliases[catchKey] === e }, e)));
  const applyCatch = () => {
    const next = { ...aliases };
    delete next[catchKey];
    if (catchOn.checked && catchSel.value) next[catchKey] = catchSel.value;
    save(next).catch((e) => toast(e.message, true));
  };
  catchOn.onchange = applyCatch;
  catchSel.onchange = () => catchOn.checked && applyCatch();

  const rows = Object.entries(aliases).filter(([k]) => !k.startsWith("@")).sort().map(([k, v]) => h("tr", {},
    h("td", {}, h("span", { class: "user-cell" }, icon("at"), k)),
    h("td", {}, v),
    h("td", { class: "act" }, moreBtn(() => [
      { label: "Edit", icon: "edit", onClick: () => editModal(k, v) },
      { label: "Copy alias", icon: "copy", onClick: () => copy(k, "Alias copied") },
      "-",
      { label: "Delete", icon: "trash", danger: true, onClick: () => confirmModal("Delete alias", `Stop accepting mail for ${k}?`, "Delete", true,
        async () => { const next = { ...aliases }; delete next[k]; await save(next); }) },
    ]))));

  $("view").replaceChildren(
    pageHead("Aliases", "Extra addresses that deliver to an account",
      h("button", { class: "btn primary", onclick: () => editModal(), disabled: !emails.length }, icon("plus"), "Add alias")),
    panel(null, null, h("div", { class: "panel-body" },
      h("div", { class: "switch-row" },
        h("label", { class: "switch" }, catchOn, h("span")),
        h("div", { class: "txt" }, h("b", {}, "Catch-all"), h("span", { class: "muted" }, `Deliver mail for any unknown @${ov?.domain} address to `), catchSel)),
      h("div", { class: "switch-row" },
        icon("info"),
        h("div", { class: "txt muted" }, "Plus-addressing always works: ", h("code", {}, "name+anything@" + (ov?.domain || "")), " delivers to ", h("code", {}, "name@"))))),
    panel(null, null, h("div", { class: "table-wrap" }, h("table", {},
      h("thead", {}, h("tr", {}, h("th", {}, "Alias"), h("th", {}, "Delivers to"), h("th"))),
      h("tbody", {}, rows.length ? rows : h("tr", {}, h("td", { colspan: 3, class: "empty" }, emails.length ? "No aliases yet." : "Create an account first.")))))));
};

// Queue
views.queue = async () => {
  const items = await api("GET", "/api/queue");
  const rows = [];
  for (const it of items) {
    for (const r of it.rcpts) {
      const kind = r.status === "sent" ? "ok" : r.status === "failed" ? "bad" : "warn";
      rows.push(h("tr", {},
        h("td", {}, fmtTime(it.created)),
        h("td", {}, it.from),
        h("td", {}, r.addr),
        h("td", {}, badge(kind, r.status)),
        h("td", { class: "num" }, String(it.attempts)),
        h("td", {}, r.status === "pending" ? fmtTime(it.next_try) : "—"),
        h("td", { class: "act" }, r.last_error ? h("button", { class: "icon-btn", title: "Last error",
          onclick: (e) => { e.stopPropagation(); showPopover(e.currentTarget, () => [
            h("div", { class: "pop-head" }, icon("alert"), "Last error"),
            h("div", { class: "pop-body mono small" }, r.last_error)]); } }, icon("info")) : null)));
    }
  }
  $("view").replaceChildren(
    pageHead("Outbound queue", "Retries for 4 days, then bounces to the sender",
      h("button", { class: "btn", disabled: !rows.length, onclick: async (e) => withBusy(e.currentTarget, async () => {
        await api("POST", "/api/queue/retry");
        toast("Retrying now");
        setTimeout(render, 2500);
      }) }, icon("refresh"), "Retry all")),
    panel(null, null, h("div", { class: "table-wrap" }, h("table", {},
      h("thead", {}, h("tr", {}, h("th", {}, "Queued"), h("th", {}, "From"), h("th", {}, "To"), h("th", {}, "Status"),
        h("th", { class: "num" }, "Attempts"), h("th", {}, "Next try"), h("th"))),
      h("tbody", {}, rows.length ? rows : h("tr", {}, h("td", { colspan: 7, class: "empty" }, "Nothing waiting to send.")))))));
};

// Logs
views.logs = async () => {
  let paused = false;
  const filter = h("input", { type: "search", placeholder: "Filter" });
  const box = h("pre", { class: "logbox" });
  const pauseBtn = h("button", { class: "icon-btn", title: "Pause" }, icon("pause"));
  let lines = [];
  const draw = () => {
    const q = filter.value.toLowerCase();
    const atBottom = box.scrollTop + box.clientHeight >= box.scrollHeight - 30;
    box.replaceChildren(...lines.filter((l) => !q || l.toLowerCase().includes(q)).map((l) =>
      h("div", { class: /error|failed|denied|reject/i.test(l) ? "err" : "" }, l)));
    if (atBottom) box.scrollTop = box.scrollHeight;
  };
  const tick = async () => {
    if (current !== "logs") return;
    if (!paused) {
      lines = await api("GET", "/api/logs").catch(() => lines);
      draw();
    }
    logTimer = setTimeout(tick, 3000);
  };
  pauseBtn.onclick = () => {
    paused = !paused;
    pauseBtn.replaceChildren(icon(paused ? "play" : "pause"));
    pauseBtn.title = paused ? "Resume" : "Pause";
  };
  filter.oninput = draw;
  $("view").replaceChildren(
    pageHead("Logs", "Live",
      h("div", { class: "search" }, icon("search"), filter),
      pauseBtn,
      h("button", { class: "icon-btn", title: "Copy all", onclick: () => copy(lines.join("\n"), "Logs copied") }, icon("copy"))),
    panel(null, null, box));
  await tick();
  box.scrollTop = box.scrollHeight;
};

// Settings
views.settings = async () => {
  const s = await api("GET", "/api/settings");
  const put = async (patch) => {
    Object.assign(s, patch);
    await api("PUT", "/api/settings", s);
    toast("Settings saved");
  };
  const toggle = (key, title, desc) => {
    const cb = h("input", { type: "checkbox", checked: s[key] });
    cb.onchange = () => put({ [key]: cb.checked }).catch((e) => { cb.checked = !cb.checked; toast(e.message, true); });
    return h("div", { class: "switch-row" }, h("label", { class: "switch" }, cb, h("span")),
      h("div", { class: "txt" }, h("b", {}, title), h("span", { class: "muted" }, desc)));
  };
  const max = h("input", { type: "number", min: 1, max: 25, value: s.max_message_mb, style: "width:80px" });
  max.onchange = () => put({ max_message_mb: Number(max.value) }).catch((e) => toast(e.message, true));

  const cf = ov?.checks?.cloudflare || {};
  const ts = ov?.checks?.tailscale || {};
  const connRow = (ic, name, detail, kind, ...buttons) => h("tr", {},
    h("td", {}, h("span", { class: "user-cell" }, icon(ic), h("b", {}, name))),
    h("td", { class: "wrap muted" }, detail),
    h("td", {}, badge(kind, kindText[kind])),
    h("td", { class: "act" }, buttons));
  const st = statusOf();

  $("view").replaceChildren(
    pageHead("Settings"),
    panel("Mail handling", "mail", h("div", { class: "panel-body" },
      toggle("reject_dmarc_failures", "Reject DMARC failures", "Refuse mail that fails the sender's DMARC policy when it says reject. When off, it goes to Junk."),
      toggle("save_sent_copy", "Save sent copies", "File a copy of every sent message in Sent. Leave off for Outlook, which does this itself."),
      h("div", { class: "switch-row" }, h("div", { class: "txt" }, h("b", {}, "Max message size"),
        h("span", { class: "muted" }, "Largest message accepted, in MB. Cloudflare's limit is 25.")), max))),
    panel("Connections", "server", h("div", { class: "table-wrap" }, h("table", {}, h("tbody", {},
      connRow("cloud", "Cloudflare", cf.connected ? `${cf.domain} · connected ${fmtTime(ov.connected_at)}` : "Not connected", st.cf,
        moreBtn(() => cf.connected ? [
          { label: "Repair setup", icon: "refresh", onClick: repairCloudflare },
          { label: "Use a different token", icon: "key", onClick: () => openWizard(1) },
        ] : [{ label: "Set up", icon: "plus", onClick: () => openWizard(1) }])),
      connRow("globe", "Tailscale Funnel", ts.dns_name ? `${ts.dns_name} · :443 IMAP, :8443 SMTP` : "Not enabled", st.ts,
        moreBtn(() => [{ label: ts.funnel_imap ? "Re-apply Funnel" : "Enable Funnel", icon: "refresh", onClick: () => enableTailscale() }])))))),
    panel("Admin access", "info", h("div", { class: "panel-body muted" },
      "This page only listens on ", h("code", {}, location.host), " and can't be reached from other devices.")));
};

// ---------- start ----------

(async () => {
  try {
    await loadOverview();
  } catch (err) {
    toast(err.message, true);
  }
  render();
  let shown = false;
  try { shown = sessionStorage.getItem("mailer.wiz") === "1"; sessionStorage.setItem("mailer.wiz", "1"); } catch { /* ignore */ }
  if (setupStep() === 1 && !shown) openWizard();
  setInterval(async () => {
    try {
      await loadOverview();
      if (current === "dashboard" && !$("modal").open) render();
    } catch { /* server restarting */ }
  }, 20000);
})();
