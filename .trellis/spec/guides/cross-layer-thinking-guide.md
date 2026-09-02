# 跨层思考指南

> 多数缺陷出在边界：env 与文件谁说了算、节点对象是否脱敏、host 模式是否 fail-closed、API `code` 是否有 i18n。

---

## 先画数据流

典型路径：

```
环境变量 / --flag
    → config.Load + Validate
        → main 装配
            → manager 状态机 ↔ tunnel/egress/health
            → state JSON 文件
            → webui REST/SSE
                → static/app.js
```

每条箭头问：格式是什么、谁校验、失败时用户看到什么、磁盘上留下什么。

---

## 本仓库的边界

| 边界 | 合同 | 常见事故 |
|------|------|----------|
| env ↔ `route.json` | 磁盘赢。`New` 加载 route 覆盖 env | 改 `ROUTE_MODE` 却发现 UI 上次的 fixed 还在 |
| env ↔ `ui_auth.json` | 启动时用 env 密码重哈希；secret_path 稳定 | 轮换 secret 导致管理 URL 失效 |
| `node.Node` ↔ HTTP | Snapshot/SSE 必须 `sanitizeNode`（含 `current_node` / `target_node`） | `GET /api/nodes` 目前仍可能带 `config_text`——不要再开一条同样的口 |
| `manager.State` ↔ UI 药丸 | 字符串：`idle/fetching/connecting/connected/drifting` | 按 README 的 PROBING/STABLE 去画，药丸会一直 idle 样式 |
| API `code` ↔ i18n | 三语 `errors.<code>` | 只加了 Go 侧，界面显示 raw key |
| OpenVPN profile ↔ 进程 | `ValidateOpenVPNProfile` 在 parse 和 `prepareFiles` | 相信缓存的 `nodes.json` 里的旧配置 |
| egress ready ↔ proxy | 未就绪则拨号失败 | 用 `net.Dial`「临时救一下」导致泄漏 |
| embed 静态资源 ↔ 浏览器 | `__VER__` 来自 `versionedAssets` 哈希 | 改了 `app.js` 没重编，或新文件没进哈希列表 |
| container 默认路由 ↔ 入站 | netfix 把回包标回网桥 | 新端口（例如第二个 UI）忘了交给 `netfix.Apply` |

---

## 改字段时的清单

新增或重命名 JSON 字段（节点、快照、路由、黑名单、日志）：

- [ ] 改 Go struct tag
- [ ] 改 `sanitizeNode` / Snapshot 是否需要省略
- [ ] 改 `app.js` 读取（以及空值占位）
- [ ] 若是错误码：改 i18n 三语 + 测试
- [ ] 若是 state 字符串：改 `setPill` 与 i18n `status.*`
- [ ] 演示数据 `demoNodes` 是否也要填

新增监听端口：

- [ ] `config` + `Validate`
- [ ] `main` 里把它传给 `netfix.Apply`（container）
- [ ] Dockerfile `EXPOSE` / compose 端口
- [ ] 是否应走 egress 还是必须留在 eth0

新增 env：

- [ ] `config.Load` 默认值
- [ ] `.env.example` 与 host `conduitvpn.env.example`（不要把密码写进模板）
- [ ] README 配置表（中英若改文档再动）

---

## 校验放在哪

- **一次、在入口**：VPNGate CSV、订阅、UI JSON、`Config.Validate`
- **再一次、在使用点**：`prepareFiles` 重新校验 OpenVPN（缓存不可信）
- **不要**：proxy 热路径、前端「再 parse 一遍 profile」

---

## 反模式

- UI 直接假设 `current_node.config_text` 存在
- 让 frontend 去猜后端内部 state 机（只消费 Snapshot）
- 改 `writeJSON` 格式却不改 `Load*` 兼容
- 只更新 `zh-CN` 翻译
- 把 demo 根路径重定向抄到生产（生产 `/` 必须 404）
