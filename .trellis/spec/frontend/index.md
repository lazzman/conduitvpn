# Frontend 开发规范

> 原生 HTML / CSS / JS，无框架、无构建器。文件在 `internal/webui/static/`，由 Go `embed` 进二进制。

---

## 概览

管理后台是单页 + 登录页。脚本用相对路径打到同一 secret 前缀下的 REST 和 SSE。改 UI 必须改静态文件并**重新编译**，运行中的二进制不会热更新。

---

## 规范索引

| 文件 | 何时阅读 |
|------|----------|
| [目录结构](./directory-structure.md) | 增删静态文件、缓存破坏、embed 列表 |
| [UI 约定](./ui-conventions.md) | DOM、i18n、主题、API 调用、港澳台命名 |

Go 侧路由、会话、CSP 见 [`../backend/`](../backend/index.md)。跨层字段见 [guides](../guides/cross-layer-thinking-guide.md)。

---

## Pre-Development Checklist

- [ ] 新字符串进 `i18n.js` 的 `en` / `zh-CN` / `zh-TW`，页面用 `data-i18n`，不要写死文案
- [ ] 不用 `innerHTML` / `insertAdjacentHTML`（`TestEmbeddedScriptsDoNotUseHTMLInjection`）
- [ ] 新静态文件加入 `webui.go` 的 `versionedAssets`，否则 `__VER__` 不会变
- [ ] API 错误用响应里的 `code` 走 `ConduitI18n.errorMessage`
- [ ] 节点渲染当 `config_text` 不存在；不要把它画到 DOM 上

---

## Quality Check

- [ ] `make test` 中的 `TestEmbedded*` 仍通过
- [ ] 登录页与面板页共享 `theme.js` / `i18n.js`
- [ ] 相对 URL 仍是 `./api/...`（页面挂在 `/{secret}/` 下）
