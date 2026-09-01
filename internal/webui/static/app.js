/* Conduit VPN Gateway frontend — vanilla, no framework, one accent. */
"use strict";

const $ = (id) => document.getElementById(id);

/* ---- state pill ---- */
/* ---- state pill ---- */
function setPill(state) {
  const pill = $("status-pill");
  pill.className = "pill";
  let cls = "pill-idle";
  if (state === "connected") cls = "pill-connected";
  else if (state === "connecting" || state === "fetching") cls = "pill-connecting";
  else if (state === "drifting") cls = "pill-drifting";
  pill.classList.add(cls);
  const knownStates = ["idle", "fetching", "connecting", "connected", "drifting"];
  const label = knownStates.includes(state) ? window.ConduitI18n.t(`status.${state}`) : state.toUpperCase();
  $("status-text").textContent = label;
  pill.setAttribute("aria-label", label);
}

/* ---- uptime ---- */
let bootTs = Date.now() / 1000;
let lastState = null;

function fmtUptime(sec) {
  const s = Math.max(0, Math.floor(sec));
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  if (d > 0) return window.ConduitI18n.t("uptime.days", { days: d, hours: h });
  if (h > 0) return window.ConduitI18n.t("uptime.hours", { hours: h, minutes: m });
  return window.ConduitI18n.t("uptime.minutes", { minutes: m, seconds: s % 60 });
}

/* ---- state render ---- */
function renderState(snap) {
  lastState = snap;
  setPill(snap.state);
  bootTs = Date.now() / 1000 - snap.uptime_sec;

  const n = snap.current_node || null;
  $("c-node").textContent = n ? n.host_name : "—";
  $("c-node-sub").textContent = n
    ? `${n.country_short || "??"} · ${n.remote_host}:${n.remote_port} · ${n.remote_proto || "udp"}`
    : snap.detail || "—";

  $("c-egress").textContent = n ? (n.ip || "—") : "—";
  $("c-egress-sub").textContent = n
    ? [countryRegionName(n.country_short, n.country_long), n.operator].filter(Boolean).join(" · ") || "—"
    : "—";

  $("c-proxy").textContent = snap.proxy_port || "7928";
  $("c-uptime").textContent = fmtUptime(snap.uptime_sec);
  $("c-blacklist").textContent = window.ConduitI18n.t("blacklist.count", { count: snap.blacklist_count || 0 });
}

/* ---- blacklist manager ---- */
let blacklistEntries = {};
let blacklistTest = { running: false, total: 0, completed: 0, results: [] };
let blacklistPollTimer = null;

function formatBlacklistTime(value) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(window.ConduitI18n.locale(), {
    year: "numeric", month: "2-digit", day: "2-digit",
    hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
  }).format(date);
}

function renderBlacklistManager() {
  const tbody = document.querySelector("#blacklist-table tbody");
  const entries = Object.entries(blacklistEntries).sort(([a], [b]) => a.localeCompare(b));
  const resultByHost = new Map((blacklistTest.results || []).map((r) => [r.host, r]));
  const nodeByHost = new Map(nodes.map((n) => [n.host_name, n]));
  const completed = blacklistTest.completed || 0;
  const total = blacklistTest.total || entries.length;
  $("blacklist-progress").textContent = blacklistTest.running
    ? window.ConduitI18n.t("blacklist.progressRunning", { completed, total })
    : (blacklistTest.started_at ? window.ConduitI18n.t("blacklist.progressDone", { completed, total }) : (total ? window.ConduitI18n.t("blacklist.notTested") : window.ConduitI18n.t("blacklist.empty")));

  $("btn-blacklist-test").disabled = blacklistTest.running || entries.length === 0;
  $("btn-blacklist-test").textContent = window.ConduitI18n.t(blacklistTest.running ? "blacklist.testing" : "blacklist.test");
  const passed = entries.filter(([host]) => resultByHost.get(host)?.status === "passed").length;
  $("btn-blacklist-restore").disabled = blacklistTest.running || passed === 0;

  tbody.replaceChildren();
  if (entries.length === 0) {
    const tr = document.createElement("tr");
    const td = document.createElement("td");
    td.colSpan = 5;
    td.className = "empty-note";
    td.textContent = window.ConduitI18n.t("blacklist.empty");
    tr.appendChild(td);
    tbody.appendChild(tr);
    return;
  }
  const cell = (className, value, title = "") => {
    const td = document.createElement("td");
    td.className = className;
    td.textContent = value;
    if (title) td.title = title;
    return td;
  };
  for (const [host, entry] of entries) {
    const tr = document.createElement("tr");
    const n = nodeByHost.get(host);
    const nodeText = n
      ? `${countryRegionName(n.country_short, n.country_long) || "—"} · ${n.ip || "—"}`
      : window.ConduitI18n.t("blacklist.nodeMissing");
    const result = resultByHost.get(host) || { status: "pending" };
    const label = window.ConduitI18n.t(`blacklist.${result.status || "unknown"}`);
    const detail = result.code ? window.ConduitI18n.errorMessage(result) : label;
    tr.appendChild(cell("host-cell", host, host));
    tr.appendChild(cell("ip-cell", nodeText, nodeText));
    tr.appendChild(cell("", entry.reason || "—", entry.reason || ""));
    tr.appendChild(cell("mono", formatBlacklistTime(entry.marked_at)));
    tr.appendChild(cell(`blacklist-result ${result.status}`, detail, detail));
    tbody.appendChild(tr);
  }
}

async function loadBlacklistManager() {
  try {
    const [entriesResponse, testResponse] = await Promise.all([
      fetch("./api/blacklist"),
      fetch("./api/actions/test-blacklist"),
    ]);
    if (!entriesResponse.ok || !testResponse.ok) return;
    blacklistEntries = await entriesResponse.json();
    blacklistTest = await testResponse.json();
    renderBlacklistManager();
    clearTimeout(blacklistPollTimer);
    if ($("blacklist-dialog").open && blacklistTest.running) {
      blacklistPollTimer = setTimeout(loadBlacklistManager, 900);
    }
  } catch (_) {}
}

$("btn-blacklist").addEventListener("click", () => {
  const dialog = $("blacklist-dialog");
  dialog.showModal();
  loadBlacklistManager();
});

$("btn-blacklist-close").addEventListener("click", () => $("blacklist-dialog").close());
$("blacklist-dialog").addEventListener("close", () => {
  clearTimeout(blacklistPollTimer);
  blacklistPollTimer = null;
});

$("btn-blacklist-test").addEventListener("click", async () => {
  try {
    const response = await fetch("./api/actions/test-blacklist", { method: "POST" });
    if (!response.ok) return;
    await loadBlacklistManager();
  } catch (_) {}
});

$("btn-blacklist-restore").addEventListener("click", async () => {
  try {
    const response = await fetch("./api/actions/restore-available-blacklist", { method: "POST" });
    if (!response.ok) return;
    await Promise.all([
      loadBlacklistManager(),
      loadNodes(),
      fetch("./api/state").then((r) => r.ok ? r.json() : null).then((snap) => { if (snap) renderState(snap); }),
    ]);
  } catch (_) {}
});

/* ---- VPNGate source settings ---- */
let sourceStatus = null;
let sourcePollTimer = null;
let sourceDirty = false;

function formatSourceTime(value) {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : new Intl.DateTimeFormat(window.ConduitI18n.locale(), {
    year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
  }).format(date);
}

function renderSourceStatus(status) {
  if (!status) return;
  sourceStatus = status;
  $("sources-official").value = status.official_url || "—";
  $("sources-current").textContent = status.current_source || window.ConduitI18n.t("sources.none");
  $("sources-last-attempt").textContent = formatSourceTime(status.last_attempt_at);
  $("sources-last-success").textContent = formatSourceTime(status.last_success_at);
  const attempts = status.attempts || [];
  $("sources-attempts").replaceChildren();
  for (const attempt of attempts) {
    const line = document.createElement("div");
    line.className = attempt.ok ? "source-attempt ok" : "source-attempt failed";
    line.textContent = window.ConduitI18n.t("sources.attempt", {
      url: attempt.url,
      result: attempt.ok ? window.ConduitI18n.t("sources.ok") : (attempt.error || window.ConduitI18n.t("sources.failed")),
    });
    $("sources-attempts").appendChild(line);
  }
  const message = $("sources-save-msg");
  if (status.refreshing && !message.classList.contains("err")) {
    message.textContent = window.ConduitI18n.t("sources.refreshing");
    message.classList.remove("err");
    message.dataset.refreshing = "true";
  } else if (message.dataset.refreshing === "true") {
    message.textContent = "";
    delete message.dataset.refreshing;
  }
}

function scheduleSourceStatusPoll() {
  clearTimeout(sourcePollTimer);
  if ($("sources-dialog").open) sourcePollTimer = setTimeout(() => loadSourceStatus(false), 900);
}

function loadSourceStatus(syncMirrors = false) {
  return fetch("./api/vpngate-sources")
    .then((response) => response.ok ? response.json() : null)
    .then((status) => {
      if (!status) {
        if ($("sources-dialog").open) scheduleSourceStatusPoll();
        return null;
      }
      renderSourceStatus(status);
      if (syncMirrors && !sourceDirty) $("sources-text").value = (status.mirrors || []).join("\n");
      clearTimeout(sourcePollTimer);
      if ($("sources-dialog").open) scheduleSourceStatusPoll();
      return status;
    })
    .catch(() => {
      if ($("sources-dialog").open) scheduleSourceStatusPoll();
      return null;
    });
}

function showSourceInputMessage(message, error = false) {
  const node = $("sources-input-msg");
  node.textContent = message || "";
  node.classList.toggle("err", error);
}

function normalizePastedURLs(text) {
  const sources = [];
  const seen = new Set();
  const starts = [];
  const startRe = /\bhttps?:\/\//gi;
  let match;
  while ((match = startRe.exec(text)) !== null) starts.push(match.index);
  let coveredUntil = 0;
  for (const start of starts) {
    if (start < coveredUntil) continue;
    const segment = cutPastedURLSegment(text.slice(start));
    coveredUntil = start + segment.length;
    let token = segment;
    token = trimPastedURLToken(token);
    try {
      const url = new URL(token);
      if (!["http:", "https:"].includes(url.protocol) || (url.password && !url.username)) continue;
      const rawHost = url.hostname.toLowerCase().replace(/\.$/, "");
      const host = rawHost.includes(":") && !rawHost.startsWith("[") ? `[${rawHost}]` : rawHost;
      const keepPort = url.port && !((url.protocol === "http:" && url.port === "80") || (url.protocol === "https:" && url.port === "443"));
      const origin = url.protocol + "//" + host + (keepPort ? ":" + url.port : "");
      const userinfo = url.username
        ? url.username + (url.password ? ":" + url.password : "") + "@"
        : "";
      const source = url.protocol + "//" + userinfo + host + (keepPort ? ":" + url.port : "");
      if (!seen.has(origin)) { seen.add(origin); sources.push(source); }
    } catch (_) {}
  }
  return sources;
}

function cutPastedURLSegment(segment) {
  const schemeEnd = segment.search(/:\/\//);
  for (let i = 0; i < segment.length; i++) {
    const ch = segment[i];
    if (/[\s<>"'()（）},;|，；、｜]/u.test(ch)) return segment.slice(0, i);
    if ((ch === "[" || ch === "{" || ch === "【") && (schemeEnd < 0 || i > schemeEnd + 3)) return segment.slice(0, i);
  }
  return segment;
}

function trimPastedURLToken(token) {
  while (token) {
    const last = token[token.length - 1];
    if (/[.,;:!?\}>'"`’‘”“〉》】》，。；：！？…]/u.test(last)) {
      token = token.slice(0, -1);
      continue;
    }
    if (last === ")" && [...token].filter((c) => c === ")").length > [...token].filter((c) => c === "(").length) {
      token = token.slice(0, -1);
      continue;
    }
    if (last === "]" && [...token].filter((c) => c === "]").length > [...token].filter((c) => c === "[").length) {
      token = token.slice(0, -1);
      continue;
    }
    if (last === "）" && [...token].filter((c) => c === "）").length > [...token].filter((c) => c === "（").length) {
      token = token.slice(0, -1);
      continue;
    }
    if (last === "】" && [...token].filter((c) => c === "】").length > [...token].filter((c) => c === "【").length) {
      token = token.slice(0, -1);
      continue;
    }
    break;
  }
  return token;
}

$("sources-text").addEventListener("paste", (event) => {
  event.preventDefault();
  const text = event.clipboardData?.getData("text") || "";
  const input = $("sources-text");
  const before = input.value.slice(0, input.selectionStart);
  const after = input.value.slice(input.selectionEnd);
  if (normalizePastedURLs(text).length === 0) {
    showSourceInputMessage(window.ConduitI18n.t("sources.noURL"), true);
    return;
  }
  const sources = normalizePastedURLs(before + text + after);
  if (sources.length === 0) {
    showSourceInputMessage(window.ConduitI18n.t("sources.noURL"), true);
    return;
  }
  input.value = sources.join("\n");
  sourceDirty = true;
  showSourceInputMessage("");
});

$("sources-text").addEventListener("input", () => {
  sourceDirty = true;
  showSourceInputMessage("");
});

$("btn-sources").addEventListener("click", () => {
  $("sources-dialog").showModal();
  const message = $("sources-save-msg");
  message.textContent = "";
  message.classList.remove("err");
  delete message.dataset.refreshing;
  sourceDirty = false;
  loadSourceStatus(true);
});
$("btn-sources-close").addEventListener("click", () => $("sources-dialog").close());
$("btn-sources-cancel").addEventListener("click", () => $("sources-dialog").close());
$("sources-dialog").addEventListener("close", () => {
  clearTimeout(sourcePollTimer);
  sourcePollTimer = null;
  showSourceInputMessage("");
  const message = $("sources-save-msg");
  message.textContent = "";
  message.classList.remove("err");
  delete message.dataset.refreshing;
  sourceDirty = false;
});

$("btn-sources-save").addEventListener("click", async () => {
  const button = $("btn-sources-save");
  button.disabled = true;
  const message = $("sources-save-msg");
  message.textContent = window.ConduitI18n.t("action.applying");
  message.classList.remove("err");
  delete message.dataset.refreshing;
  try {
    const response = await fetch("./api/vpngate-sources", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ text: $("sources-text").value }),
    });
    const result = await response.json();
    if (!response.ok) {
      message.textContent = result.error || window.ConduitI18n.t("sources.saveFailed");
      message.classList.add("err");
      return;
    }
    $("sources-text").value = (result.mirrors || []).join("\n");
    sourceDirty = false;
    const issues = result.issues || [];
    showSourceInputMessage(issues.length ? window.ConduitI18n.t("sources.filtered", { count: issues.length }) : "", Boolean(issues.length));
    message.textContent = window.ConduitI18n.t("sources.saved");
    message.classList.remove("err");
    await loadSourceStatus(false);
    scheduleSourceStatusPoll();
  } catch (_) {
    message.textContent = window.ConduitI18n.t("errors.network");
    message.classList.add("err");
  } finally {
    button.disabled = false;
  }
});

/* ---- node table ---- */
let nodes = [];
let sortKey = "latency";
let sortDir = 1;
let filterText = "";
let filterCountry = "";
let filterSource = "";
let filterAttr = "";

function countryRegionName(countryShort, fallback) {
  return window.ConduitI18n.regionName(countryShort, fallback || "");
}

function purityOf(n) {
  return n && n.purity ? n.purity : null;
}

function displayCountryCode(n) {
  const p = purityOf(n);
  return String((p && p.country) || n.country_short || "").toUpperCase();
}

function sourceKey(n) {
  const p = purityOf(n);
  return String((p && p.source) || "").toLowerCase();
}

function attrList(n) {
  const p = purityOf(n);
  return Array.isArray(p && p.attrs) ? p.attrs : [];
}

function sourceLabel(source) {
  const key = {
    isp: "nodes.sourceIsp",
    hosting: "nodes.sourceHosting",
    business: "nodes.sourceBusiness",
    education: "nodes.sourceEducation",
  }[source];
  return key ? window.ConduitI18n.t(key) : (source || "—");
}

function attrLabel(attr) {
  const key = {
    vpn: "nodes.attrVpn",
    proxy: "nodes.attrProxy",
    tor: "nodes.attrTor",
    relay: "nodes.attrRelay",
    hosting: "nodes.attrHosting",
    mobile: "nodes.attrMobile",
    anycast: "nodes.attrAnycast",
    anonymous: "nodes.attrAnonymous",
    satellite: "nodes.attrSatellite",
  }[attr];
  return key ? window.ConduitI18n.t(key) : attr;
}

function fillSelect(id, options, selected, allLabel) {
  const el = $(id);
  const previous = selected;
  el.replaceChildren();
  const all = document.createElement("option");
  all.value = "";
  all.textContent = allLabel;
  el.appendChild(all);
  options.forEach((opt) => {
    const option = document.createElement("option");
    option.value = opt.value;
    option.textContent = opt.label;
    el.appendChild(option);
  });
  el.value = options.some((opt) => opt.value === previous) ? previous : "";
  return el.value;
}

function populateNodeFilters() {
  const countries = {};
  const sources = {};
  const attrs = {};
  nodes.forEach((n) => {
    const cc = displayCountryCode(n);
    if (cc) countries[cc] = countryRegionName(cc, n.country_long);
    const src = sourceKey(n);
    if (src) sources[src] = sourceLabel(src);
    attrList(n).forEach((a) => { attrs[a] = attrLabel(a); });
  });
  const allLabel = window.ConduitI18n.t("nodes.filterAll");
  filterCountry = fillSelect(
    "filter-country",
    Object.keys(countries).sort().map((code) => ({ value: code, label: code + " · " + countries[code] })),
    filterCountry,
    allLabel
  );
  filterSource = fillSelect(
    "filter-source",
    Object.keys(sources).sort().map((value) => ({ value: value, label: sources[value] })),
    filterSource,
    allLabel
  );
  filterAttr = fillSelect(
    "filter-attr",
    Object.keys(attrs).sort().map((value) => ({ value: value, label: attrs[value] })),
    filterAttr,
    allLabel
  );
}

function sortValue(n) {
  const p = purityOf(n);
  if (sortKey === "latency") return n.tested && n.latency_ms > 0 ? n.latency_ms : Infinity;
  if (sortKey === "country") return displayCountryCode(n);
  if (sortKey === "source") return sourceKey(n);
  if (sortKey === "attrs") return attrList(n).join(",");
  if (sortKey === "postal") return (p && (p.postal || p.city)) || "";
  if (sortKey === "host") return n.host_name || "";
  if (sortKey === "ip") return n.ip || "";
  if (sortKey === "proto") return n.remote_proto || "";
  return n[sortKey] ?? "";
}

function loadNodes() {
  fetch("./api/nodes")
    .then((r) => r.json())
    .then((list) => { nodes = list; populateNodeFilters(); renderTable(); populateRouteSelects(); })
    .catch(() => {});
}

function populateRouteSelects() {
  // country chips: distinct countries, sorted by code, multi-select
  const cc = $("route-country-list");
  const seen = {};
  nodes.forEach((n) => { if (n.country_short) seen[n.country_short] = true; });
  const codes = Object.keys(seen).sort();
  cc.replaceChildren();
  codes.forEach((c) => {
    const node = nodes.find((n) => String(n.country_short || "").toUpperCase() === c);
    const name = countryRegionName(c, node?.country_long);
    const chip = document.createElement("button");
    chip.type = "button";
    chip.className = "cc-chip";
    chip.dataset.code = c;
    chip.title = name;
    chip.textContent = `${c} · ${name}`;
    cc.appendChild(chip);
  });
  cc.querySelectorAll(".cc-chip").forEach((chip) => {
    chip.addEventListener("click", () => chip.classList.toggle("selected"));
  });
  applyChipSelection();

  // node dropdown: hostname · latency · ping, sorted by latency
  const nn = $("route-node");
  const rows = nodes
    .map((n) => ({ n, lat: n.tested && n.latency_ms > 0 ? n.latency_ms : Infinity }))
    .sort((a, b) => a.lat - b.lat);
  nn.replaceChildren();
  const empty = document.createElement("option");
  empty.value = "";
  empty.textContent = window.ConduitI18n.t("route.selectNode");
  nn.appendChild(empty);
  rows.forEach(({ n, lat }) => {
    const latTxt = lat === Infinity ? "—" : `${lat}ms`;
    const option = document.createElement("option");
    option.value = n.host_name || "";
    option.dataset.ip = n.ip || "";
    option.textContent = `${n.host_name || ""} · ${latTxt} · ping ${n.ping ?? "—"}`;
    nn.appendChild(option);
  });
  if (routeCfg.mode === "fixed" && routeCfg.node) nn.value = routeCfg.node;
}

function renderTable() {
  const q = filterText.toLowerCase();
  let rows = nodes.filter((n) => {
    const p = purityOf(n);
    const cc = displayCountryCode(n);
    if (filterCountry && cc !== filterCountry) return false;
    if (filterSource && sourceKey(n) !== filterSource) return false;
    if (filterAttr && attrList(n).indexOf(filterAttr) < 0) return false;
    if (!q) return true;
    const hay = [
      n.host_name, n.ip, n.country_short, n.country_long, n.remote_proto, cc,
      sourceKey(n), sourceLabel(sourceKey(n)), attrList(n).join(" "),
      p && p.postal, p && p.city, p && p.org,
    ].join(" ").toLowerCase();
    return hay.includes(q);
  });

  rows.sort((a, b) => {
    const va = sortValue(a);
    const vb = sortValue(b);
    if (typeof va === "number" && typeof vb === "number") return (va - vb) * sortDir;
    return String(va).localeCompare(String(vb)) * sortDir;
  });

  $("node-count").textContent = rows.length;

  const tbody = document.querySelector("#node-table tbody");
  tbody.replaceChildren();
  if (rows.length === 0) {
    const tr = document.createElement("tr");
    const td = document.createElement("td");
    td.colSpan = 11;
    td.className = "empty-note";
    td.textContent = window.ConduitI18n.t("nodes.empty");
    tr.appendChild(td);
    tbody.appendChild(tr);
    return;
  }

  for (const n of rows) {
    const lat = n.tested && n.latency_ms > 0 ? n.latency_ms + "ms" : "—";
    const latCls = n.tested && n.latency_ms > 0
      ? (n.latency_ms < 200 ? "lat-ok" : n.latency_ms < 500 ? "lat-mid" : "lat-dead")
      : "lat-dead";
    const p = purityOf(n);
    const countryCode = displayCountryCode(n) || "??";
    const country = countryRegionName(countryCode, n.country_long);
    const vgCode = String(n.country_short || "").toUpperCase();
    const countryTitle = vgCode && vgCode !== countryCode ? country + " · VPNGate " + vgCode : country;
    const tr = document.createElement("tr");
    const cell = (className, value, title) => {
      const td = document.createElement("td");
      td.className = className;
      td.textContent = value;
      if (title) td.title = title;
      return td;
    };
    tr.appendChild(cell("num " + latCls, lat));
    const countryCell = cell("country-cell", "", countryTitle);
    const code = document.createElement("span");
    code.className = "country-code";
    code.textContent = countryCode;
    const countryName = document.createElement("span");
    countryName.className = "country-name";
    countryName.textContent = country;
    countryCell.append(code, countryName);
    tr.appendChild(countryCell);

    const src = sourceKey(n);
    const sourceCell = cell("source-cell", "", src);
    if (!p || p.status === "pending") {
      sourceCell.textContent = window.ConduitI18n.t("nodes.purityPending");
      sourceCell.classList.add("purity-pending");
    } else if (p.status === "error") {
      sourceCell.textContent = window.ConduitI18n.t("nodes.purityError");
      sourceCell.classList.add("purity-error");
    } else {
      sourceCell.textContent = sourceLabel(src);
      if (p.hosting || src === "hosting") sourceCell.classList.add("source-hosting");
      else if (src === "isp") sourceCell.classList.add("source-isp");
    }
    tr.appendChild(sourceCell);

    const attrsCell = cell("attrs-cell", "");
    const labels = attrList(n);
    if (labels.length === 0) {
      attrsCell.textContent = "—";
    } else {
      labels.forEach((a) => {
        const tag = document.createElement("span");
        tag.className = "attr-tag" + (a === "hosting" || a === "vpn" ? " attr-" + a : "");
        tag.textContent = attrLabel(a);
        attrsCell.appendChild(tag);
      });
    }
    tr.appendChild(attrsCell);

    const postal = (p && (p.postal || p.city)) || "—";
    tr.appendChild(cell("postal-cell", postal, p && p.city ? [p.city, p.region].filter(Boolean).join(", ") : postal));
    tr.appendChild(cell("host-cell", n.host_name || "", n.host_name || ""));
    tr.appendChild(cell("ip-cell", n.ip || "", n.ip || ""));
    tr.appendChild(cell("proto-cell", n.remote_proto || "udp"));
    tr.appendChild(cell("num", String(n.score ?? "—")));
    tr.appendChild(cell("num", String(n.ping ?? "—")));
    const action = cell("act-col", "");
    const lock = document.createElement("button");
    lock.className = "btn btn-ghost btn-lock";
    lock.dataset.host = n.host_name || "";
    lock.title = window.ConduitI18n.t("nodes.lockTitle");
    lock.textContent = window.ConduitI18n.t("nodes.lock");
    action.appendChild(lock);
    tr.appendChild(action);
    tbody.appendChild(tr);
  }

  tbody.querySelectorAll(".btn-lock").forEach((btn) => {
    btn.addEventListener("click", () => setRoute("fixed", "", btn.dataset.host));
  });
}

document.querySelectorAll("#node-table thead th").forEach((th) => {
  th.addEventListener("click", () => {
    const key = th.dataset.sort;
    if (!key) return;
    if (sortKey === key) sortDir = -sortDir;
    else { sortKey = key; sortDir = 1; }
    document.querySelectorAll("#node-table thead th").forEach((t) => {
      const hint = t.querySelector(".sort-hint");
      if (hint) hint.textContent = t === th ? (sortDir > 0 ? "↑" : "↓") : "";
    });
    renderTable();
  });
});

$("node-filter").addEventListener("input", (e) => {
  filterText = e.target.value;
  renderTable();
});
["filter-country", "filter-source", "filter-attr"].forEach((id) => {
  $(id).addEventListener("change", (e) => {
    if (id === "filter-country") filterCountry = e.target.value;
    if (id === "filter-source") filterSource = e.target.value;
    if (id === "filter-attr") filterAttr = e.target.value;
    renderTable();
  });
});


/* ---- route mode ---- */
let routeCfg = { mode: "auto", country: "", node: "" };

// highlight chips matching the comma-separated routeCfg.country
function applyChipSelection() {
  const selected = new Set((routeCfg.country || "").split(",").map((x) => x.trim()).filter(Boolean));
  document.querySelectorAll("#route-country-list .cc-chip").forEach((chip) => {
    chip.classList.toggle("selected", selected.has(chip.dataset.code));
  });
}

function renderRoute() {
  document.querySelectorAll("#route-seg .seg-btn").forEach((b) => {
    b.classList.toggle("active", b.dataset.mode === routeCfg.mode);
  });
  $("route-country-list").hidden = routeCfg.mode !== "country";
  $("route-node").hidden = routeCfg.mode !== "fixed";
  applyChipSelection();
  if (routeCfg.node) $("route-node").value = routeCfg.node;
  const detail = routeCfg.mode === "country"
    ? " · " + (routeCfg.country ? routeCfg.country.split(",").map((c) => countryRegionName(c)).join(" / ") : window.ConduitI18n.t("route.none"))
    : routeCfg.mode === "fixed" ? " · " + routeCfg.node : "";
  const label = routeCfg.mode === "auto" ? window.ConduitI18n.t("route.autoDetail") : window.ConduitI18n.t(`route.${routeCfg.mode}`);
  $("route-status").textContent = label + detail;
}

function loadRoute() {
  fetch("./api/route")
    .then((r) => r.json())
    .then((cfg) => { routeCfg = cfg; renderRoute(); })
    .catch(() => {});
}

function setRoute(mode, country, node) {
  const body = {
    mode,
    country: country !== undefined ? country
      : (mode === "country" ? selectedCountries().join(",") : routeCfg.country),
    node: node !== undefined ? node : (mode === "fixed" ? $("route-node").value : routeCfg.node),
  };
  $("route-msg").textContent = window.ConduitI18n.t("action.applying");
  fetch("./api/route", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
    .then((r) => r.json())
    .then((res) => {
      if (res.error) {
        $("route-msg").textContent = window.ConduitI18n.errorMessage(res);
        $("route-msg").classList.add("err");
      } else {
        $("route-msg").textContent = window.ConduitI18n.t("action.applied");
        $("route-msg").classList.remove("err");
        loadRoute();
      }
    })
    .catch(() => { $("route-msg").textContent = window.ConduitI18n.t("errors.network"); })
    .finally(() => setTimeout(() => { $("route-msg").textContent = ""; }, 2500));
}

function selectedCountries() {
  return Array.from(document.querySelectorAll("#route-country-list .cc-chip.selected"))
    .map((c) => c.dataset.code);
}

document.querySelectorAll("#route-seg .seg-btn").forEach((btn) => {
  btn.addEventListener("click", () => {
    document.querySelectorAll("#route-seg .seg-btn").forEach((b) => b.classList.toggle("active", b === btn));
    $("route-country-list").hidden = btn.dataset.mode !== "country";
    $("route-node").hidden = btn.dataset.mode !== "fixed";
  });
});

$("btn-route-apply").addEventListener("click", () => {
  const mode = document.querySelector("#route-seg .seg-btn.active").dataset.mode;
  setRoute(mode);
});

/* ---- real-time latency chart ---- */
const CHART_POINTS = 60; // ~2min at 2s state cadence
let latSeries = [];
let latMin = null, latMax = null;

function pushLatency(ms) {
  const v = (ms && ms > 0) ? ms : null;
  latSeries.push(v);
  if (latSeries.length > CHART_POINTS) latSeries.shift();
  const vals = latSeries.filter((x) => x != null);
  latMin = vals.length ? Math.min(...vals) : null;
  latMax = vals.length ? Math.max(...vals) : null;
  drawChart();
  const last = vals[vals.length - 1];
  const node = (routeCfg && routeCfg.mode === "fixed") ? "" : "";
  $("lat-summary").textContent = last != null
    ? window.ConduitI18n.t("latency.current", { last, max: latMax, count: vals.length })
    : (vals.length ? window.ConduitI18n.t("latency.waiting") : window.ConduitI18n.t("latency.empty"));
}

function cssVar(name) {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

function drawChart() {
  const cv = $("lat-chart");
  if (!cv) return;
  const dpr = window.devicePixelRatio || 1;
  const w = cv.clientWidth, h = cv.clientHeight;
  if (w === 0) return;
  cv.width = w * dpr;
  cv.height = h * dpr;
  const ctx = cv.getContext("2d");
  ctx.scale(dpr, dpr);
  ctx.clearRect(0, 0, w, h);

  const pad = { top: 8, right: 8, bottom: 16, left: 8 };
  const plotW = w - pad.left - pad.right;
  const plotH = h - pad.top - pad.bottom;
  if (plotW <= 0 || plotH <= 0) return;

  const vals = latSeries.filter((x) => x != null);
  const max = Math.max(120, ...vals, 1) * 1.15;
  const border = cssVar("--border") || "#1e1e24";
  const accent = cssVar("--accent") || "#0066ff";
  const text2 = cssVar("--text-3") || "#5c5c68";

  // grid + labels
  ctx.font = "9px ui-monospace, monospace";
  ctx.fillStyle = text2;
  ctx.strokeStyle = border;
  ctx.lineWidth = 1;
  for (let g = 0; g <= 2; g++) {
    const y = pad.top + (g / 2) * plotH;
    ctx.beginPath();
    ctx.moveTo(pad.left, y);
    ctx.lineTo(w - pad.right, y);
    ctx.stroke();
    const label = Math.round(max * (1 - g / 2));
    ctx.fillText(`${label}ms`, pad.left + 2, y - 2);
  }

  // baseline at current value
  const lastVal = vals[vals.length - 1];
  if (lastVal != null) {
    const y = pad.top + plotH - (lastVal / max) * plotH;
    ctx.setLineDash([3, 3]);
    ctx.strokeStyle = accent;
    ctx.globalAlpha = 0.4;
    ctx.beginPath();
    ctx.moveTo(pad.left, y);
    ctx.lineTo(w - pad.right, y);
    ctx.stroke();
    ctx.setLineDash([]);
    ctx.globalAlpha = 1;
  }

  // series line
  ctx.strokeStyle = accent;
  ctx.lineWidth = 1.6;
  ctx.lineJoin = "round";
  ctx.beginPath();
  let started = false;
  for (let i = 0; i < latSeries.length; i++) {
    const v = latSeries[i];
    if (v == null) { started = false; continue; }
    const x = pad.left + (i / (CHART_POINTS - 1)) * plotW;
    const y = pad.top + plotH - (v / max) * plotH;
    if (!started) { ctx.moveTo(x, y); started = true; }
    else ctx.lineTo(x, y);
  }
  ctx.stroke();

  // last point dot
  if (lastVal != null) {
    const x = pad.left + ((latSeries.length - 1) / (CHART_POINTS - 1)) * plotW;
    const y = pad.top + plotH - (lastVal / max) * plotH;
    ctx.fillStyle = accent;
    ctx.beginPath();
    ctx.arc(x, y, 2.5, 0, Math.PI * 2);
    ctx.fill();
  }
}

/* ---- logs ---- */


let logLevel = "all";
let follow = true;
let lastLogs = [];

const LEVELS = { debug: 1, info: 2, warn: 3, error: 4 };
function formatLogTime(timestamp) {
  const time = new Date(timestamp);
  if (Number.isNaN(time.getTime())) return "";
  return new Intl.DateTimeFormat(window.ConduitI18n.locale(), {
    year: "numeric", month: "2-digit", day: "2-digit",
    hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false,
  }).format(time);
}

function appendLog(entry) {
  const view = $("log-view");
  const atBottom = view.scrollHeight - view.scrollTop - view.clientHeight < 24;
  const lvl = entry.lvl || "info";
  const line = document.createElement("span");
  line.className = `log-line lvl-${lvl}`;
  const ts = formatLogTime(entry.ts);
  const kv = Object.entries(entry)
    .filter(([k]) => k !== "ts" && k !== "lvl" && k !== "msg")
    .map(([k, v]) => `${k}=${v}`)
    .join(" ");
  const time = document.createElement("span");
  time.className = "t";
  time.textContent = ts;
  line.appendChild(time);
  line.append(` [${lvl.toUpperCase()}] ${entry.msg || ""}${kv ? " " + kv : ""}`);
  view.appendChild(line);
  if (view.childNodes.length > 800) view.removeChild(view.firstChild);
  if (follow && atBottom) view.scrollTop = view.scrollHeight;
}

function renderLogs(list) {
  lastLogs = list;
  const view = $("log-view");
  view.replaceChildren();
  const visible = list.filter((e) => logLevel === "all" || LEVELS[e.lvl || "info"] >= LEVELS[logLevel]);
  if (visible.length === 0) {
    const empty = document.createElement("span");
    empty.className = "empty-note";
    empty.textContent = window.ConduitI18n.t("logs.empty");
    view.appendChild(empty);
    return;
  }
  for (const e of visible) appendLog(e);
  view.scrollTop = view.scrollHeight;
}

function loadLogs() {
  fetch("./api/logs?n=200")
    .then((r) => r.json())
    .then(renderLogs)
    .catch(() => {});
}

document.querySelectorAll("#log-chips .chip").forEach((chip) => {
  chip.addEventListener("click", () => {
    document.querySelectorAll("#log-chips .chip").forEach((c) => c.classList.remove("active"));
    chip.classList.add("active");
    logLevel = chip.dataset.level;
    loadLogs();
  });
});

$("btn-clear-log").addEventListener("click", () => {
  $("log-view").replaceChildren();
});

$("btn-scroll").addEventListener("click", () => {
  follow = !follow;
  $("btn-scroll").classList.toggle("active", follow);
});

$("log-view").addEventListener("scroll", () => {
  const view = $("log-view");
  follow = view.scrollHeight - view.scrollTop - view.clientHeight < 24;
  $("btn-scroll").classList.toggle("active", follow);
});

/* ---- actions ---- */
$("btn-refresh").addEventListener("click", () => {
  const btn = $("btn-refresh");
  btn.disabled = true;
  btn.textContent = window.ConduitI18n.t("action.refreshing");
  fetch("./api/actions/update-nodes", { method: "POST" })
    .catch(() => {})
    .finally(() => {
      btn.disabled = false;
      btn.textContent = window.ConduitI18n.t("action.refresh");
    });
});

/* ---- SSE ---- */
function connectStream() {
  const es = new EventSource("./api/logs/stream");
  es.onmessage = (ev) => {
    try {
      const msg = JSON.parse(ev.data);
      if (msg.type === "log") {
        appendLog(msg.payload);
      } else if (msg.type === "state") {
        renderState(msg.payload);
        const cn = msg.payload.current_node;
        pushLatency(cn ? cn.latency_ms : null);
      }
    } catch (_) {}
  };
  es.onerror = () => {
    es.close();
    setTimeout(connectStream, 3000); // auto-reconnect
  };
}

/* ---- boot ---- */
document.addEventListener("conduit-theme-change", drawChart);
document.addEventListener("conduit-language-change", () => {
  if (lastState) renderState(lastState);
  populateNodeFilters();
  renderTable();
  populateRouteSelects();
  renderRoute();
  renderBlacklistManager();
  renderLogs(lastLogs);
  if (sourceStatus && $("sources-dialog").open) renderSourceStatus(sourceStatus);
  const vals = latSeries.filter((x) => x != null);
  const last = vals[vals.length - 1];
  $("lat-summary").textContent = last != null
    ? window.ConduitI18n.t("latency.current", { last, max: latMax, count: vals.length })
    : (vals.length ? window.ConduitI18n.t("latency.waiting") : window.ConduitI18n.t("latency.empty"));
  drawChart();
});
window.ConduitTheme.initControls();
fetch("./api/state")
  .then((r) => r.json())
  .then((snap) => {
    renderState(snap);
    const cn = snap.current_node;
    pushLatency(cn ? cn.latency_ms : null);
  })
  .catch(() => {});

// uptime ticker
setInterval(() => {
  $("c-uptime").textContent = fmtUptime(Date.now() / 1000 - bootTs);
}, 1000);

// state poller: keeps status + latency chart dense even when proxies
// throttle the SSE stream (e.g. Cloudflare buffering).
setInterval(() => {
  fetch("./api/state")
    .then((r) => r.json())
    .then((snap) => {
      renderState(snap);
      const cn = snap.current_node;
      pushLatency(cn ? cn.latency_ms : null);
      if ((snap.purity_pending || 0) > 0) loadNodes();
    })
    .catch(() => {});
}, 3000);
setInterval(loadNodes, 5000);

loadNodes();
loadLogs();
loadRoute();
connectStream();
