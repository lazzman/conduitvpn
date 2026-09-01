# Frontend 目录结构

---

## 文件

```
internal/webui/static/
  index.html      管理面板；占位 __VER__、__DEMO_HINT__ 由 Go 替换
  login.html      登录页
  styles.css      单一设计令牌（--accent、--text-*、data-theme）
  app.js          面板逻辑：状态、节点表、路由、黑名单、日志、SSE
  login.js        登录提交
  i18n.js         ConduitI18n：文案、regionName、errorMessage
  theme.js        ConduitTheme：system/dark/light
  site.webmanifest
  品牌图标 / favicon
```

Go 侧 `//go:embed static` 以及 `versionedAssets` 列表决定缓存破坏哈希。**增删或重命名静态文件时同步改 `versionedAssets`**，否则 `staticAssetVersion` 测不过，CDN 也可能继续提供旧 JS/CSS 组合。

HTML 里的脚本样式引用写成 `./app.js?v=__VER__`。`serveIndex` / `serveLogin` 把 `__VER__` 换成 16 位 hex。不要改成绝对 `/static/` 路径——生产环境根路径是 404，真实前缀是 `/{secret}/`。

---

## 职责划分

| 文件 | 拥有 |
|------|------|
| `app.js` | 面板状态、表格、图表、SSE、动作按钮 |
| `login.js` | POST `./api/login` |
| `i18n.js` | 字典、语言菜单、`regionName`、`errorMessage` |
| `theme.js` | `data-theme`、localStorage `conduit-theme` |
| `styles.css` | 布局与令牌；不要在 JS 里拼大量内联样式，图表除外 |

共享启动：`ConduitTheme.initControls()`；语言变化发 `conduit-language-change`，主题变化发 `conduit-theme-change`。面板监听它们来重绘文案和延迟图。

---

## 反模式

- 引入 React/Vue/Vite 或 `package.json`
- 把第三方 CDN 脚本加进 CSP（CSP 是 `script-src 'self'`）
- 新增 HTML 页却不 embed、不加进 `versionedAssets`
