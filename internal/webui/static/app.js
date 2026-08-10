/* Conduit VPN Gateway frontend — vanilla, no framework, one accent. */
"use strict";

const $ = (id) => document.getElementById(id);

/* ---- theme ---- */
// Three-way theme: system (follow OS) / dark / light, persisted.
const themeSel = $("btn-theme");
const darkQuery = window.matchMedia("(prefers-color-scheme: dark)");
let theme = localStorage.getItem("conduit-theme") || "system";
if (!["system", "dark", "light"].includes(theme)) theme = "system";

function effectiveTheme() {
  return theme === "system" ? (darkQuery.matches ? "dark" : "light") : theme;
}

function applyTheme() {
  const eff = effectiveTheme();
  document.documentElement.dataset.theme = eff;
  document.querySelector('meta[name="color-scheme"]').setAttribute(
    "content", theme === "system" ? "dark light" : eff);
  themeSel.value = theme;
}
themeSel.addEventListener("change", () => {
  theme = themeSel.value;
  localStorage.setItem("conduit-theme", theme);
  applyTheme();
  drawChart();
});
darkQuery.addEventListener("change", () => {
  if (theme === "system") { applyTheme(); drawChart(); }
});
applyTheme();

/* ---- state pill ---- */
/* ---- state pill ---- */
const STATE_LABEL = {
  idle: "IDLE",
  fetching: "FETCHING",
  connecting: "CONNECTING",
  connected: "CONNECTED",
  drifting: "DRIFTING",
};

function setPill(state) {
  const pill = $("status-pill");
  pill.className = "pill";
  let cls = "pill-idle";
  if (state === "connected") cls = "pill-connected";
  else if (state === "connecting" || state === "fetching") cls = "pill-connecting";
  else if (state === "drifting") cls = "pill-drifting";
  pill.classList.add(cls);
  $("status-text").textContent = STATE_LABEL[state] || state.toUpperCase();
}

/* ---- uptime ---- */
let bootTs = Date.now() / 1000;

function fmtUptime(sec) {
  const s = Math.max(0, Math.floor(sec));
  const d = Math.floor(s / 86400), h = Math.floor((s % 86400) / 3600), m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m ${s % 60}s`;
}

/* ---- state render ---- */
function renderState(snap) {
  setPill(snap.state);
  bootTs = Date.now() / 1000 - snap.uptime_sec;

  const n = snap.current_node || null;
  $("c-node").textContent = n ? n.host_name : "—";
  $("c-node-sub").textContent = n
    ? `${n.country_short || "??"} · ${n.remote_host}:${n.remote_port} · ${n.remote_proto || "udp"}`
    : snap.detail || "—";

  $("c-egress").textContent = n ? (n.ip || "—") : "—";
  $("c-egress-sub").textContent = n ? `${n.country_long || ""} · ${n.operator || ""}`.trim() : "—";

  $("c-proxy").textContent = snap.proxy_port || "7928";
  $("c-uptime").textContent = fmtUptime(snap.uptime_sec);
  $("c-blacklist").textContent = `${snap.blacklist_count || 0} 个节点已拉黑`;
}

/* ---- node table ---- */
let nodes = [];
let sortKey = "latency";
let sortDir = 1;
let filterText = "";

const COUNTRY_LONG = {
  JP: "日本", KR: "韩国", US: "美国", GB: "英国", DE: "德国", FR: "法国", NL: "荷兰",
  SG: "新加坡", CA: "加拿大", AU: "澳大利亚", HK: "香港", TW: "台湾", SE: "瑞典", FI: "芬兰",
  TH: "泰国", IN: "印度", VN: "越南", MY: "马来西亚", ID: "印尼", PH: "菲律宾",
  IT: "意大利", ES: "西班牙", CH: "瑞士", PL: "波兰", RO: "罗马尼亚", TR: "土耳其",
  BR: "巴西", MX: "墨西哥", AR: "阿根廷", RU: "俄罗斯", UA: "乌克兰", CZ: "捷克",
  NO: "挪威", DK: "丹麦", IE: "爱尔兰", PT: "葡萄牙", BE: "比利时", AT: "奥地利",
  GR: "希腊", IL: "以色列", AE: "阿联酋", SA: "沙特", NZ: "新西兰", ZA: "南非",
  EE: "爱沙尼亚", LV: "拉脱维亚", LT: "立陶宛", BG: "保加利亚", HU: "匈牙利",
};

function loadNodes() {
  fetch("./api/nodes")
    .then((r) => r.json())
    .then((list) => { nodes = list; renderTable(); populateRouteSelects(); })
    .catch(() => {});
}

function populateRouteSelects() {
  // country chips: distinct countries, sorted by code, multi-select
  const cc = $("route-country-list");
  const seen = {};
  nodes.forEach((n) => { if (n.country_short) seen[n.country_short] = true; });
  const codes = Object.keys(seen).sort();
  cc.innerHTML = codes.map((c) => {
    const name = COUNTRY_LONG[c] || (nodes.find(n => n.country_short === c)?.country_long || c);
    return `<button type="button" class="cc-chip" data-code="${c}" title="${name}">${c} · ${name}</button>`;
  }).join("");
  cc.querySelectorAll(".cc-chip").forEach((chip) => {
    chip.addEventListener("click", () => chip.classList.toggle("selected"));
  });
  applyChipSelection();

  // node dropdown: hostname · latency · ping, sorted by latency
  const nn = $("route-node");
  const rows = nodes
    .map((n) => ({ n, lat: n.tested && n.latency_ms > 0 ? n.latency_ms : Infinity }))
    .sort((a, b) => a.lat - b.lat);
  nn.innerHTML = '<option value="">选择节点…</option>' + rows.map(({ n, lat }) => {
    const latTxt = lat === Infinity ? "—" : `${lat}ms`;
    return `<option value="${n.host_name}" data-ip="${n.ip}">${n.host_name} · ${latTxt} · ping ${n.ping ?? "—"}</option>`;
  }).join("");
  if (routeCfg.mode === "fixed" && routeCfg.node) nn.value = routeCfg.node;
}

function renderTable() {
  const q = filterText.toLowerCase();
  let rows = nodes.filter((n) => {
    if (!q) return true;
    return [n.host_name, n.ip, n.country_short, n.country_long, n.remote_proto]
      .join(" ").toLowerCase().includes(q);
  });

  rows.sort((a, b) => {
    let va = a[sortKey] ?? "", vb = b[sortKey] ?? "";
    if (sortKey === "latency") {
      va = a.tested && a.latency_ms > 0 ? a.latency_ms : Infinity;
      vb = b.tested && b.latency_ms > 0 ? b.latency_ms : Infinity;
    }
    if (typeof va === "number" && typeof vb === "number") return (va - vb) * sortDir;
    return String(va).localeCompare(String(vb)) * sortDir;
  });

  $("node-count").textContent = rows.length;

  const tbody = document.querySelector("#node-table tbody");
  tbody.innerHTML = "";
  if (rows.length === 0) {
    const tr = document.createElement("tr");
    tr.innerHTML = `<td colspan="8" class="empty-note">暂无节点 — 点击右上角「更新节点」拉取</td>`;
    tbody.appendChild(tr);
    return;
  }

  for (const n of rows) {
    const lat = n.tested && n.latency_ms > 0 ? `${n.latency_ms}ms` : "—";
    const latCls = n.tested && n.latency_ms > 0
      ? (n.latency_ms < 200 ? "lat-ok" : n.latency_ms < 500 ? "lat-mid" : "lat-dead")
      : "lat-dead";
    const country = n.country_short || "??";
    const tr = document.createElement("tr");
    tr.innerHTML = `
      <td class="num ${latCls}">${lat}</td>
      <td><span class="country-code">${country}</span></td>
      <td class="host-cell" title="${n.host_name}">${n.host_name}</td>
      <td class="ip-cell" title="${n.ip}">${n.ip}</td>
      <td class="proto-cell">${n.remote_proto || "udp"}</td>
      <td class="num">${n.score ?? "—"}</td>
      <td class="num">${n.ping ?? "—"}</td>
      <td class="act-col"><button class="btn btn-ghost btn-lock" data-host="${n.host_name}" title="锁定此节点（固定 IP 模式）">锁定</button></td>`;
    tbody.appendChild(tr);
  }

  // lock button: switch to fixed mode targeting this node
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
      t.querySelector(".sort-hint").textContent = t === th ? (sortDir > 0 ? "↑" : "↓") : "";
    });
    renderTable();
  });
});

$("node-filter").addEventListener("input", (e) => {
  filterText = e.target.value;
  renderTable();
});


/* ---- route mode ---- */
let routeCfg = { mode: "auto", country: "", node: "" };

const ROUTE_LABEL = {
  auto: "智能自动配置",
  country: "固定国家",
  fixed: "固定 IP",
};

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
    ? " · " + (routeCfg.country ? routeCfg.country.split(",").map((c) => COUNTRY_LONG[c] || c).join(" / ") : "未选择")
    : routeCfg.mode === "fixed" ? " · " + routeCfg.node : "";
  $("route-status").textContent = (ROUTE_LABEL[routeCfg.mode] || routeCfg.mode) + detail;
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
  $("route-msg").textContent = "应用中…";
  fetch("./api/route", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
    .then((r) => r.json())
    .then((res) => {
      if (res.error) {
        $("route-msg").textContent = res.error;
        $("route-msg").classList.add("err");
      } else {
        $("route-msg").textContent = "已应用";
        $("route-msg").classList.remove("err");
        loadRoute();
      }
    })
    .catch(() => { $("route-msg").textContent = "请求失败"; })
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
    ? `当前 ${last}ms · 峰值 ${latMax}ms · 采样 ${vals.length}`
    : (vals.length ? "等待采样…" : "暂无数据");
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
  const accent = cssVar("--accent") || "#34d399";
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

const LEVELS = { debug: 1, info: 2, warn: 3, error: 4 };

function appendLog(entry) {
  const view = $("log-view");
  const atBottom = view.scrollHeight - view.scrollTop - view.clientHeight < 24;
  const lvl = entry.lvl || "info";
  const line = document.createElement("span");
  line.className = `log-line lvl-${lvl}`;
  const ts = (entry.ts || "").slice(11, 19) || "";
  const kv = Object.entries(entry)
    .filter(([k]) => k !== "ts" && k !== "lvl" && k !== "msg")
    .map(([k, v]) => `${k}=${v}`)
    .join(" ");
  line.innerHTML = `<span class="t">${ts}</span> [${lvl.toUpperCase()}] ${entry.msg || ""}${kv ? " " + kv : ""}`;
  view.appendChild(line);
  if (view.childNodes.length > 800) view.removeChild(view.firstChild);
  if (follow && atBottom) view.scrollTop = view.scrollHeight;
}

function renderLogs(list) {
  const view = $("log-view");
  view.innerHTML = "";
  const visible = list.filter((e) => logLevel === "all" || LEVELS[e.lvl || "info"] >= LEVELS[logLevel]);
  if (visible.length === 0) {
    view.innerHTML = `<span class="empty-note">暂无日志</span>`;
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
  $("log-view").innerHTML = "";
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
  btn.textContent = "更新中…";
  fetch("./api/actions/update-nodes", { method: "POST" })
    .catch(() => {})
    .finally(() => {
      btn.disabled = false;
      btn.textContent = "更新节点";
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
    })
    .catch(() => {});
}, 3000);

loadNodes();
loadLogs();
loadRoute();
connectStream();
