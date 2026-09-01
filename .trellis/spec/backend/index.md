# Backend 开发规范

> 适用于 `cmd/conduitvpn/` 与 `internal/` 下的 Go 代码。本仓库是单模块、零第三方依赖的静态二进制。

---

## 概览

ConduitVPN 把 VPNGate 公共 OpenVPN 节点变成常驻代理网关。Go 侧负责配置、监督状态机、隧道/子进程、代理入站、JSON 状态和 Web API。前端静态资源由 `internal/webui` 通过 `embed` 打进同一二进制。

不要按「路由 / 服务 / ORM」的 Web 框架模板来组织代码。新逻辑放进已有 `internal/<pkg>`，入口装配只发生在 `cmd/conduitvpn/main.go`。

---

## 规范索引

| 文件 | 何时阅读 |
|------|----------|
| [目录结构](./directory-structure.md) | 新增包、移动文件、决定逻辑归属 |
| [错误处理](./error-handling.md) | 返回 error、写 HTTP API、fail-closed 路径 |
| [日志](./logging-guidelines.md) | 任何 `logx` 调用或 SSE 日志字段 |
| [质量与约束](./quality-guidelines.md) | 依赖、测试、脱敏、已知边界 |
| [状态持久化](./state-persistence.md) | 读写 data dir、凭据、黑名单、路由 |
| [网络模式](./network-modes.md) | `container` / `host`、egress、netfix、hy2 |
| [子进程](./subprocess.md) | openvpn / sing-box 生命周期 |

跨层（API ↔ UI、隧道 ↔ 代理、env ↔ 持久化）先看 [`.trellis/spec/guides/cross-layer-thinking-guide.md`](../guides/cross-layer-thinking-guide.md)。

---

## Pre-Development Checklist

动手前按变更类型阅读对应文件：

- [ ] 改包布局或新增 `internal/` 包 → `directory-structure.md`
- [ ] 改配置、监听、凭据校验 → `quality-guidelines.md` + `error-handling.md`
- [ ] 改隧道、代理拨号、hy2、netfix → `network-modes.md` + `subprocess.md`
- [ ] 改 `nodes.json` / `blacklist.json` / `route.json` / `ui_auth.json` → `state-persistence.md`
- [ ] 改 REST / SSE 响应中的节点字段 → `quality-guidelines.md`（脱敏）+ `error-handling.md`
- [ ] 打日志 → `logging-guidelines.md`
- [ ] 始终过一遍 `quality-guidelines.md` 的硬约束（零依赖、CGO、IPv4）

---

## Quality Check

提交或声称完成前：

- [ ] `go.mod` 仍只有 `module conduitvpn` + `go 1.22`，没有新 `require`
- [ ] 没有把 OpenVPN `config_text` / 证书 / 私钥写进 API、日志或 SSE
- [ ] host 模式没有走默认路由泄漏；隧道未就绪时拨号失败而不是直连
- [ ] 子进程仍有握手超时、进程组 SIGTERM、超时 SIGKILL、回收
- [ ] 为新分支补了同包 `*_test.go`（风格对齐现有测试）
- [ ] `make vet` 与相关 `go test` 能过（宿主机无 Go 时用 `make test`）
