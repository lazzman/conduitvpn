# 思考指南

> 写代码前用来扩展问题，而不是通用最佳实践清单。内容针对 ConduitVPN 的真实边界。

---

## 何时打开

| 指南 | 用途 | 触发 |
|------|------|------|
| [代码复用](./code-reuse-thinking-guide.md) | 找到已经存在的拨号、日志、校验、子进程入口 | 你准备新写一段「看起来很熟」的逻辑 |
| [跨层](./cross-layer-thinking-guide.md) | 沿 env → manager → 文件 → API → UI 走一遍数据 | 改字段、状态、路由模式或安全边界 |

---

## 快速触发

跨层：

- [ ] 新 API 字段或新 `manager.State` 字符串
- [ ] 节点对象离开进程（REST、SSE、日志）
- [ ] `NETWORK_MODE` 行为变化
- [ ] env 默认值与 `route.json` / `ui_auth.json` 交互
- [ ] 前端要展示后端新 `code`

复用：

- [ ] 新的 `exec.Command`
- [ ] 新的 `net.Dial` / `http.Client`
- [ ] 新的 JSON 文件
- [ ] 新的错误返回给浏览器
- [ ] 复制了 `sanitizeNode` 或 `writeJSON`

改常量前先搜：

```bash
rg "value_to_change" --type go -g '*.js' -g '*.html'
```

---

## 开发前

1. 读对应 `backend/` 或 `frontend/` 规范。
2. 变更跨包时读跨层指南。
3. 准备新辅助函数时读复用指南。
