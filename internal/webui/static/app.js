/* AimiliVPN Gateway frontend — vanilla, no framework, one accent. */
"use strict";

const $ = (id) => document.getElementById(id);

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
};

function loadNodes() {
  fetch("./api/nodes")
    .then((r) => r.json())
    .then((list) => { nodes = list; renderTable(); })
    .catch(() => {});
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
    tr.innerHTML = `<td colspan="7" class="empty-note">暂无节点 — 点击右上角「更新节点」拉取</td>`;
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
      <td class="num">${n.ping ?? "—"}</td>`;
    tbody.appendChild(tr);
  }
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
  .then((snap) => { renderState(snap); setInterval(() => {
    $("c-uptime").textContent = fmtUptime(Date.now() / 1000 - bootTs);
  }, 1000); })
  .catch(() => {});

loadNodes();
loadLogs();
connectStream();
