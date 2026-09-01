# UI 约定

> 原生 DOM、`"use strict"`、无构建步骤。

---

## DOM 与 XSS

用 `document.createElement` + `textContent` / `replaceChildren`。日志、节点主机名、拉黑原因都是不可信字符串。

禁止 `innerHTML` 和 `insertAdjacentHTML`。`webui_test.go` 的 `TestEmbeddedScriptsDoNotUseHTMLInjection` 会扫描四个 JS 文件。`i18n.t` 会剥掉字典里残留的 `<span>`，不要把 HTML 塞进翻译。

取节点：`const $ = (id) => document.getElementById(id);`

---

## i18n

三种语言：`zh-CN`、`zh-TW`、`en`。键必须三种都有。页面标记：

- `data-i18n`、`data-i18n-title`、`data-i18n-aria-label`、`data-i18n-placeholder`、`data-page-title`

动态文案用 `window.ConduitI18n.t(key, { param })`。API 失败用 `ConduitI18n.errorMessage(payload)`，按 `code` 映射到 `errors.*`。

港澳台显示名走 `regionName`，不要用 `Intl.DisplayNames` 的默认值：

```js
HK / MO / TW → 中国香港 / 中国澳门 / 中国台湾
en → Hong Kong, China / Macao, China / Taiwan, China
```

`TestEmbeddedI18nUI` 会锁这些英文字符串。改地区命名时三种语言一起改。

---

## API 与实时性

相对路径，带 cookie：

```js
fetch("./api/state")
fetch("./api/nodes")
fetch("./api/route", { method: "PUT", headers: { "Content-Type": "application/json" }, body: JSON.stringify(...) })
new EventSource("./api/logs/stream")
```

SSE `data` 为 `{type, payload}`：`log` 追加日志，`state` 刷新药丸和延迟点。`EventSource.onerror` 关闭后 3 秒重连。另有 3 秒 `./api/state` 轮询，避免代理缓冲 SSE（注释里写了 Cloudflare）。

未登录时浏览器会收到登录页或 401。登录页 `login.js` POST `./api/login`。不要自己实现 token 头——会话是 HttpOnly cookie。

状态字符串与后端一致：`idle`、`fetching`、`connecting`、`connected`、`drifting`。药丸 class：`pill-idle` / `pill-connecting` / `pill-connected` / `pill-drifting`。

---

## 主题与图表

`theme.js` 把 `document.documentElement.dataset.theme` 设为 `dark` 或 `light`。图表颜色读 CSS 变量 `--accent`、`--border`、`--text-3`，这样主题切换不用硬编码色板。延迟序列约 60 点；断开用 `null` 打断折线。

---

## 反模式

- `fetch("/api/state")` 绝对路径（会打到返回 404 的根上）
- 在 `app.js` 里复制一份错误文案而不走 i18n
- 把 `config_text` 填进 `<pre>` 或下载按钮
- 用 `eval` 或 `new Function` 解析 SSE
