/* Shared localization for the login and management pages. */
"use strict";

(() => {
  const storageKey = "conduit-language";
  const supported = ["zh-CN", "zh-TW", "en"];
  const messages = {
    en: {
      "page.gateway": "ConduitVPN · Gateway", "page.login": "ConduitVPN · Sign in",
      "language.select": "Select language", "language.menu": "Open language menu",
      "theme.menu": "Open theme menu", "theme.system": "Theme: System", "theme.dark": "Theme: Dark", "theme.light": "Theme: Light",
      "gateway.status": "Gateway status", "action.refresh": "Update nodes", "action.refreshTitle": "Fetch and benchmark nodes now", "action.refreshing": "Updating...", "action.apply": "Apply", "action.applying": "Applying...", "action.applied": "Applied", "action.failed": "Request failed",
      "status.idle": "IDLE", "status.fetching": "FETCHING", "status.connecting": "CONNECTING", "status.connected": "CONNECTED", "status.drifting": "DRIFTING",
      "card.node": "Current node", "card.egress": "Tunnel egress", "card.proxy": "Proxy port", "card.uptime": "Uptime", "card.proxySub": "HTTP + SOCKS5 · Single port", "card.switching": "Connecting {target}, then switching from {current}", "blacklist.count": "{count} node(s) blacklisted",
      "blacklist.open": "Manage blacklist", "route.section": "Routing mode", "route.autoDetail": "Smart automatic configuration", "route.auto": "Smart", "route.country": "Fixed country / region", "route.fixed": "Fixed IP", "route.none": "None selected", "route.selectNode": "Select a node...",
      "latency.section": "Live latency", "latency.current": "Current {last}ms · Peak {max}ms · Samples {count}", "latency.waiting": "Waiting for samples...", "latency.empty": "No data",
      "nodes.section": "Nodes", "nodes.filter": "Filter...", "nodes.empty": "No nodes. Use Update nodes in the top right to fetch them.", "nodes.latency": "Latency", "nodes.country": "Country / region", "nodes.host": "Host", "nodes.ip": "IP", "nodes.protocol": "Protocol", "nodes.score": "Score", "nodes.ping": "Ping", "nodes.actions": "Actions", "nodes.lock": "Lock", "nodes.lockTitle": "Lock this node (Fixed IP mode)", "nodes.source": "Source", "nodes.attrs": "Attributes", "nodes.postal": "Postal", "nodes.filterAll": "All", "nodes.filterCountry": "Country", "nodes.filterSource": "Source", "nodes.filterAttr": "Attribute", "nodes.sourceIsp": "Residential", "nodes.sourceHosting": "Datacenter", "nodes.sourceBusiness": "Business", "nodes.sourceEducation": "Education", "nodes.attrVpn": "VPN", "nodes.attrProxy": "Proxy", "nodes.attrTor": "Tor", "nodes.attrRelay": "Relay", "nodes.attrHosting": "Hosting", "nodes.attrMobile": "Mobile", "nodes.attrAnycast": "Anycast", "nodes.attrAnonymous": "Anonymous", "nodes.attrSatellite": "Satellite", "nodes.purityPending": "Checking...", "nodes.purityError": "Lookup failed",
      "logs.section": "Logs", "logs.all": "All", "logs.warn": "Warnings", "logs.error": "Errors", "logs.follow": "Follow", "logs.followTitle": "Auto-scroll", "logs.clear": "Clear", "logs.empty": "No logs",
      "blacklist.title": "Blacklist management", "blacklist.close": "Close blacklist management", "blacklist.test": "Test all", "blacklist.testing": "Testing...", "blacklist.restore": "Restore available nodes", "blacklist.host": "Host", "blacklist.node": "Node", "blacklist.reason": "Blacklist reason", "blacklist.time": "Blacklisted at", "blacklist.result": "Verification result", "blacklist.empty": "No blacklisted nodes", "blacklist.progressRunning": "Verifying {completed} / {total}", "blacklist.progressDone": "Completed {completed} / {total}", "blacklist.notTested": "Not tested", "blacklist.nodeMissing": "Not in the current node pool", "blacklist.pending": "Pending", "blacklist.running": "Verifying...", "blacklist.passed": "Available", "blacklist.failed": "Unavailable", "blacklist.skipped": "Cannot verify", "blacklist.unknown": "Not tested",
      "login.title": "Management console", "login.subtitle": "Enter your administrator credentials to continue", "login.username": "Username", "login.password": "Password", "login.submit": "Sign in", "login.demoHint": "Demo account: admin <span>Password: demo</span>",
      "errors.unauthorized": "Your session has expired. Please sign in again.", "errors.method_not_allowed": "This action is not allowed.", "errors.rate_limited": "Too many attempts. Please try again later.", "errors.invalid_json": "Invalid request data.", "errors.auth_not_initialized": "Authentication is not configured.", "errors.login_failed": "Sign-in failed.", "errors.session_capacity": "The session limit has been reached.", "errors.route_invalid": "The routing configuration is invalid.", "errors.blacklist_test_running": "Blacklist verification is already running.", "errors.too_many_log_streams": "Too many log streams are open.", "errors.verification_failed": "Verification failed.", "errors.node_not_found": "The node is not in the current node pool.", "errors.internal_error": "An internal error occurred.", "errors.unknown": "The request could not be completed.", "errors.network": "Network error. Please try again.",
      "uptime.days": "{days}d {hours}h", "uptime.hours": "{hours}h {minutes}m", "uptime.minutes": "{minutes}m {seconds}s",
    },
    "zh-CN": {
      "page.gateway": "ConduitVPN · 网关", "page.login": "ConduitVPN · 登录", "language.select": "选择语言", "language.menu": "打开语言菜单", "theme.menu": "打开主题菜单", "theme.system": "主题：跟随系统", "theme.dark": "主题：深色", "theme.light": "主题：浅色", "gateway.status": "网关状态", "action.refresh": "更新节点", "action.refreshTitle": "立即拉取并测速", "action.refreshing": "更新中…", "action.apply": "应用", "action.applying": "应用中…", "action.applied": "已应用", "action.failed": "请求失败", "status.idle": "空闲", "status.fetching": "拉取中", "status.connecting": "连接中", "status.connected": "已连接", "status.drifting": "漂移中", "card.node": "当前节点", "card.egress": "隧道出口", "card.proxy": "代理端口", "card.uptime": "运行时长", "card.proxySub": "HTTP + SOCKS5 · 单端口", "card.switching": "正在连接 {target}，成功后再从 {current} 切过去", "blacklist.count": "已拉黑 {count} 个节点", "blacklist.open": "管理黑名单", "route.section": "路由模式", "route.autoDetail": "智能自动配置", "route.auto": "智能", "route.country": "固定国家地区", "route.fixed": "固定 IP", "route.none": "未选择", "route.selectNode": "选择节点…", "latency.section": "实时延迟", "latency.current": "当前 {last}ms · 峰值 {max}ms · 采样 {count}", "latency.waiting": "等待采样…", "latency.empty": "暂无数据", "nodes.section": "节点", "nodes.filter": "过滤…", "nodes.empty": "暂无节点 — 点击右上角“更新节点”拉取", "nodes.latency": "延迟", "nodes.country": "国家地区", "nodes.host": "主机", "nodes.ip": "IP", "nodes.protocol": "协议", "nodes.score": "分数", "nodes.ping": "Ping", "nodes.actions": "操作", "nodes.lock": "锁定", "nodes.lockTitle": "锁定此节点（固定 IP 模式）", "nodes.source": "来源", "nodes.attrs": "属性", "nodes.postal": "邮编", "nodes.filterAll": "全部", "nodes.filterCountry": "国家", "nodes.filterSource": "来源", "nodes.filterAttr": "属性", "nodes.sourceIsp": "家宽", "nodes.sourceHosting": "机房", "nodes.sourceBusiness": "企业", "nodes.sourceEducation": "教育", "nodes.attrVpn": "VPN", "nodes.attrProxy": "代理", "nodes.attrTor": "Tor", "nodes.attrRelay": "中继", "nodes.attrHosting": "机房", "nodes.attrMobile": "移动", "nodes.attrAnycast": "Anycast", "nodes.attrAnonymous": "匿名", "nodes.attrSatellite": "卫星", "nodes.purityPending": "检测中...", "nodes.purityError": "检测失败", "logs.section": "日志", "logs.all": "全部", "logs.warn": "警告", "logs.error": "错误", "logs.follow": "跟随", "logs.followTitle": "自动滚动", "logs.clear": "清空", "logs.empty": "暂无日志", "blacklist.title": "黑名单管理", "blacklist.close": "关闭黑名单管理", "blacklist.test": "批量测试", "blacklist.testing": "测试中…", "blacklist.restore": "恢复可用节点", "blacklist.host": "主机", "blacklist.node": "节点", "blacklist.reason": "拉黑原因", "blacklist.time": "拉黑时间", "blacklist.result": "验证结果", "blacklist.empty": "暂无拉黑节点", "blacklist.progressRunning": "验证中 {completed} / {total}", "blacklist.progressDone": "已完成 {completed} / {total}", "blacklist.notTested": "尚未测试", "blacklist.nodeMissing": "当前节点池未找到", "blacklist.pending": "等待测试", "blacklist.running": "验证中…", "blacklist.passed": "可用", "blacklist.failed": "不可用", "blacklist.skipped": "无法验证", "blacklist.unknown": "未测试", "login.title": "管理后台", "login.subtitle": "请输入您的管理账号和安全密码以继续", "login.username": "用户名", "login.password": "密码", "login.submit": "登 录", "login.demoHint": "演示账号：admin · 密码：demo", "errors.unauthorized": "登录会话已过期，请重新登录。", "errors.method_not_allowed": "不允许此操作。", "errors.rate_limited": "尝试次数过多，请稍后重试。", "errors.invalid_json": "请求数据无效。", "errors.auth_not_initialized": "认证尚未配置。", "errors.login_failed": "登录失败。", "errors.session_capacity": "会话数量已达到上限。", "errors.route_invalid": "路由配置无效。", "errors.blacklist_test_running": "黑名单验证正在进行。", "errors.too_many_log_streams": "日志流连接过多。", "errors.verification_failed": "验证失败。", "errors.node_not_found": "当前节点池未找到该节点。", "errors.internal_error": "发生内部错误。", "errors.unknown": "请求无法完成。", "errors.network": "网络错误，请重试。", "uptime.days": "{days}天 {hours}小时", "uptime.hours": "{hours}小时 {minutes}分钟", "uptime.minutes": "{minutes}分 {seconds}秒",
    },
    "zh-TW": {
      "page.gateway": "ConduitVPN · 閘道", "page.login": "ConduitVPN · 登入", "language.select": "選擇語言", "language.menu": "開啟語言選單", "theme.menu": "開啟主題選單", "theme.system": "主題：跟隨系統", "theme.dark": "主題：深色", "theme.light": "主題：淺色", "gateway.status": "閘道狀態", "action.refresh": "更新節點", "action.refreshTitle": "立即擷取並測試節點", "action.refreshing": "更新中…", "action.apply": "套用", "action.applying": "套用中…", "action.applied": "已套用", "action.failed": "請求失敗", "status.idle": "閒置", "status.fetching": "擷取中", "status.connecting": "連線中", "status.connected": "已連線", "status.drifting": "漂移中", "card.node": "目前節點", "card.egress": "隧道出口", "card.proxy": "代理連接埠", "card.uptime": "運行時間", "card.proxySub": "HTTP + SOCKS5 · 單一連接埠", "card.switching": "正在連線 {target}，成功後再從 {current} 切過去", "blacklist.count": "已封鎖 {count} 個節點", "blacklist.open": "管理黑名單", "route.section": "路由模式", "route.autoDetail": "智慧自動設定", "route.auto": "智慧", "route.country": "固定國家／地區", "route.fixed": "固定 IP", "route.none": "未選擇", "route.selectNode": "選擇節點…", "latency.section": "即時延遲", "latency.current": "目前 {last}ms · 峰值 {max}ms · 採樣 {count}", "latency.waiting": "等待採樣…", "latency.empty": "暫無資料", "nodes.section": "節點", "nodes.filter": "篩選…", "nodes.empty": "暫無節點 — 點擊右上角「更新節點」擷取", "nodes.latency": "延遲", "nodes.country": "國家／地區", "nodes.host": "主機", "nodes.ip": "IP", "nodes.protocol": "協議", "nodes.score": "分數", "nodes.ping": "Ping", "nodes.actions": "操作", "nodes.lock": "鎖定", "nodes.lockTitle": "鎖定此節點（固定 IP 模式）", "nodes.source": "來源", "nodes.attrs": "屬性", "nodes.postal": "郵編", "nodes.filterAll": "全部", "nodes.filterCountry": "國家", "nodes.filterSource": "來源", "nodes.filterAttr": "屬性", "nodes.sourceIsp": "家寬", "nodes.sourceHosting": "機房", "nodes.sourceBusiness": "企業", "nodes.sourceEducation": "教育", "nodes.attrVpn": "VPN", "nodes.attrProxy": "代理", "nodes.attrTor": "Tor", "nodes.attrRelay": "中繼", "nodes.attrHosting": "機房", "nodes.attrMobile": "移動", "nodes.attrAnycast": "Anycast", "nodes.attrAnonymous": "匿名", "nodes.attrSatellite": "衛星", "nodes.purityPending": "檢測中...", "nodes.purityError": "檢測失敗", "logs.section": "日誌", "logs.all": "全部", "logs.warn": "警告", "logs.error": "錯誤", "logs.follow": "跟隨", "logs.followTitle": "自動捲動", "logs.clear": "清除", "logs.empty": "暫無日誌", "blacklist.title": "黑名單管理", "blacklist.close": "關閉黑名單管理", "blacklist.test": "批次測試", "blacklist.testing": "測試中…", "blacklist.restore": "恢復可用節點", "blacklist.host": "主機", "blacklist.node": "節點", "blacklist.reason": "封鎖原因", "blacklist.time": "封鎖時間", "blacklist.result": "驗證結果", "blacklist.empty": "暫無黑名單節點", "blacklist.progressRunning": "驗證中 {completed} / {total}", "blacklist.progressDone": "已完成 {completed} / {total}", "blacklist.notTested": "尚未測試", "blacklist.nodeMissing": "目前節點集區中找不到", "blacklist.pending": "等待測試", "blacklist.running": "驗證中…", "blacklist.passed": "可用", "blacklist.failed": "不可用", "blacklist.skipped": "無法驗證", "blacklist.unknown": "未測試", "login.title": "管理後台", "login.subtitle": "請輸入您的管理帳號與安全密碼以繼續", "login.username": "使用者名稱", "login.password": "密碼", "login.submit": "登 入", "login.demoHint": "示範帳號：admin · 密碼：demo", "errors.unauthorized": "登入工作階段已過期，請重新登入。", "errors.method_not_allowed": "不允許此操作。", "errors.rate_limited": "嘗試次數過多，請稍後再試。", "errors.invalid_json": "請求資料無效。", "errors.auth_not_initialized": "驗證尚未設定。", "errors.login_failed": "登入失敗。", "errors.session_capacity": "工作階段數量已達上限。", "errors.route_invalid": "路由設定無效。", "errors.blacklist_test_running": "黑名單驗證正在進行。", "errors.too_many_log_streams": "日誌串流連線過多。", "errors.verification_failed": "驗證失敗。", "errors.node_not_found": "目前節點集區中找不到該節點。", "errors.internal_error": "發生內部錯誤。", "errors.unknown": "無法完成請求。", "errors.network": "網路錯誤，請重試。", "uptime.days": "{days}天 {hours}小時", "uptime.hours": "{hours}小時 {minutes}分鐘", "uptime.minutes": "{minutes}分 {seconds}秒",
    },
  };
  const sourceMessages = {
    en: {
      "sources.open": "Configure VPNGate mirrors", "sources.title": "VPNGate sources", "sources.subtitle": "The official source is always tried first. Mirrors are fallbacks.", "sources.official": "Official source", "sources.mirrors": "Mirror URLs", "sources.authHint": "For HTTP Basic Auth, use https://<password>@host/... or https://<user>:<password>@host/... . Saved credentials are not shown again.", "sources.current": "Current source", "sources.lastAttempt": "Last attempt", "sources.lastSuccess": "Last success", "sources.save": "Save and refresh", "sources.cancel": "Cancel", "sources.close": "Close source settings", "sources.placeholder": "https://<password>@mirror.example", "sources.noURL": "No valid HTTP(S) URL found.", "sources.saved": "Saved; refresh requested.", "sources.saveFailed": "Could not save sources.", "sources.refreshing": "Refreshing sources...", "sources.none": "No successful source yet", "sources.attempt": "{url}: {result}", "sources.ok": "OK", "sources.failed": "Failed", "sources.filtered": "Ignored {count} invalid item(s).",
    },
    "zh-CN": {
      "sources.open": "配置 VPNGate 镜像", "sources.title": "VPNGate 节点源", "sources.subtitle": "官方源始终优先，镜像作为备用。", "sources.official": "官方源", "sources.mirrors": "镜像地址", "sources.authHint": "需认证时可使用 https://<密码>@host/... 或 https://<用户名>:<密码>@host/...；保存后不会回显凭据。", "sources.current": "当前使用源", "sources.lastAttempt": "最近尝试", "sources.lastSuccess": "最近成功", "sources.save": "保存并刷新", "sources.cancel": "取消", "sources.close": "关闭节点源设置", "sources.placeholder": "https://<密码>@mirror.example", "sources.noURL": "未识别到有效的 HTTP(S) 地址。", "sources.saved": "已保存，已请求刷新。", "sources.saveFailed": "节点源保存失败。", "sources.refreshing": "正在刷新节点源…", "sources.none": "尚无成功来源", "sources.attempt": "{url}：{result}", "sources.ok": "成功", "sources.failed": "失败", "sources.filtered": "已忽略 {count} 个无效内容。",
    },
    "zh-TW": {
      "sources.open": "設定 VPNGate 鏡像", "sources.title": "VPNGate 節點來源", "sources.subtitle": "官方來源永遠優先，鏡像作為備援。", "sources.official": "官方來源", "sources.mirrors": "鏡像網址", "sources.authHint": "需要驗證時可使用 https://<密碼>@host/... 或 https://<使用者名稱>:<密碼>@host/...；儲存後不會再次顯示憑據。", "sources.current": "目前使用來源", "sources.lastAttempt": "最近嘗試", "sources.lastSuccess": "最近成功", "sources.save": "儲存並刷新", "sources.cancel": "取消", "sources.close": "關閉節點來源設定", "sources.placeholder": "https://<密碼>@mirror.example", "sources.noURL": "找不到有效的 HTTP(S) 網址。", "sources.saved": "已儲存，已要求刷新。", "sources.saveFailed": "節點來源儲存失敗。", "sources.refreshing": "正在刷新節點來源…", "sources.none": "尚無成功來源", "sources.attempt": "{url}：{result}", "sources.ok": "成功", "sources.failed": "失敗", "sources.filtered": "已忽略 {count} 個無效內容。",
    },
  };
  Object.assign(messages.en, sourceMessages.en);
  Object.assign(messages["zh-CN"], sourceMessages["zh-CN"]);
  Object.assign(messages["zh-TW"], sourceMessages["zh-TW"]);
  const languageNames = { "zh-CN": "简体中文", "zh-TW": "繁體中文", en: "English" };
  let language = resolveLanguage();

  function resolveLanguage() {
    const saved = localStorage.getItem(storageKey);
    if (supported.includes(saved)) return saved;
    const candidates = navigator.languages && navigator.languages.length ? navigator.languages : [navigator.language];
    for (const candidate of candidates) {
      const value = String(candidate || "").toLowerCase();
      if (value.startsWith("zh")) return /hant|tw|hk|mo/.test(value) ? "zh-TW" : "zh-CN";
      if (value.startsWith("en")) return "en";
    }
    return "en";
  }

  function t(key, params = {}) {
    const template = messages[language][key] || messages.en[key] || key;
    return template.replace(/<\/?span>/g, "").replace(/\{(\w+)\}/g, (_, name) => String(params[name] ?? ""));
  }

  function applyPage() {
    document.documentElement.lang = language;
    document.querySelectorAll("[data-i18n]").forEach((node) => { node.textContent = t(node.dataset.i18n); });
    document.querySelectorAll("[data-i18n-title]").forEach((node) => { node.title = t(node.dataset.i18nTitle); });
    document.querySelectorAll("[data-i18n-aria-label]").forEach((node) => { node.setAttribute("aria-label", t(node.dataset.i18nAriaLabel)); });
    document.querySelectorAll("[data-i18n-placeholder]").forEach((node) => { node.placeholder = t(node.dataset.i18nPlaceholder); });
    document.querySelectorAll("[data-page-title]").forEach((node) => { document.title = t(node.dataset.pageTitle); });
  }

  function updateControl(control) {
    const button = control.querySelector("[data-language-button]");
    if (button) {
      button.title = t("language.select");
      button.setAttribute("aria-label", t("language.menu"));
    }
    control.querySelectorAll("[data-language]").forEach((option) => {
      const selected = option.dataset.language === language;
      option.classList.toggle("active", selected);
      option.setAttribute("aria-checked", String(selected));
    });
  }

  function closeMenu(control) {
    const button = control.querySelector("[data-language-button]");
    const menu = control.querySelector("[data-language-menu]");
    if (!button || !menu) return;
    menu.hidden = true;
    button.setAttribute("aria-expanded", "false");
  }

  function initControls() {
    document.querySelectorAll("[data-language-control]").forEach((control) => {
      if (control.dataset.languageReady === "true") return;
      const button = control.querySelector("[data-language-button]");
      const menu = control.querySelector("[data-language-menu]");
      if (!button || !menu) return;
      control.dataset.languageReady = "true";
      updateControl(control);
      button.addEventListener("click", (event) => {
        event.stopPropagation();
        menu.hidden = !menu.hidden;
        button.setAttribute("aria-expanded", String(!menu.hidden));
      });
      menu.addEventListener("click", (event) => {
        const option = event.target.closest("[data-language]");
        if (!option || !control.contains(option)) return;
        language = option.dataset.language;
        localStorage.setItem(storageKey, language);
        applyPage();
        document.querySelectorAll("[data-language-control]").forEach(updateControl);
        closeMenu(control);
        document.dispatchEvent(new CustomEvent("conduit-language-change", { detail: { language } }));
      });
      document.addEventListener("click", (event) => { if (!control.contains(event.target)) closeMenu(control); });
      document.addEventListener("keydown", (event) => { if (event.key === "Escape") closeMenu(control); });
    });
  }

  function locale() { return language; }
  const chinaRegionNames = {
    "zh-CN": { HK: "中国香港", MO: "中国澳门", TW: "中国台湾" },
    "zh-TW": { HK: "中國香港", MO: "中國澳門", TW: "中國台灣" },
    en: { HK: "Hong Kong, China", MO: "Macao, China", TW: "Taiwan, China" },
  };
  function regionName(code, fallback = "") {
    const country = String(code || "").trim().toUpperCase();
    if (!country) return fallback;
    if (chinaRegionNames[language]?.[country]) return chinaRegionNames[language][country];
    try { return new Intl.DisplayNames([language], { type: "region" }).of(country) || fallback || country; } catch (_) { return fallback || country; }
  }
  function errorMessage(value) { return t(`errors.${value?.code || "unknown"}`); }

  window.ConduitI18n = { t, locale, regionName, errorMessage, applyPage, initControls, languageNames };
  document.addEventListener("DOMContentLoaded", () => { applyPage(); initControls(); });
})();
