# 错误处理

> 标准库 `error` + `fmt.Errorf` 包装。没有自定义 error 层级，也没有 HTTP 框架。

---

## 传播

内部失败用 `fmt.Errorf("动作: %w", err)` 或 `errors.New`。sentinel 只在调用方需要 `errors.Is` 时导出：

- `egress.ErrTunnelNotReady` — host 模式隧道未就绪，代理必须 502 / 失败，禁止直连
- `manager.ErrBlacklistTestRunning` — 黑名单验证互斥，映射 HTTP 409
- 包内 `errModeChanged` — 路由切换中断当前连接循环，**不要**因此拉黑节点

启动期配置错误走 stderr + `os.Exit(2)`（`cfg.Validate`），运行期服务启动失败走 `logx.Error` + `os.Exit(1)`。不要在 `internal/` 里 `os.Exit`。

参考：`cmd/conduitvpn/main.go`、`internal/config/config.go` 的 `Validate`、`internal/manager/manager.go` 的 `connectLoop`。

---

## HTTP API

Web 错误统一：

```go
writeAPIError(w, status, "stable_code", "human message")
// → {"ok":false,"code":"...","error":"..."}
```

实现：`internal/webui/webui.go` 的 `writeAPIError`。稳定 `code` 由 `TestAPIErrorsHaveStableCodes` 和前端 `ConduitI18n.errorMessage` 共同约束。新增错误码必须同时改：

1. `writeAPIError` 调用点
2. `internal/webui/static/i18n.js` 里 `en` / `zh-CN` / `zh-TW` 的 `errors.<code>`
3. 至少一条 webui 测试断言 `code`

已有 HTTP `code`：`unauthorized`、`method_not_allowed`、`rate_limited`、`invalid_json`、`auth_not_initialized`、`login_failed`、`session_capacity`、`route_invalid`、`blacklist_test_running`、`too_many_log_streams`、`internal_error`。

黑名单验证结果（`BlacklistTestResult.Code`，不是 `writeAPIError`）还会用 `node_not_found`、`verification_failed`。前端同样走 `errors.*`。另有纯前端键 `unknown`、`network`。

JSON 请求用 `decodeJSON`：`MaxBytesReader` 8KiB、`DisallowUnknownFields`、拒绝多个 JSON 值。不要手写 `json.NewDecoder` 绕过这些限制。

读失败但「空列表也是合法 UI 状态」时，返回空 JSON 而不是 500：`apiNodes` / `apiBlacklist` 在缺文件时返回 `[]` / `{}`。

---

## Fail-closed

隧道不可用时，应用流量必须失败，不能从宿主机默认路由出去：

- `egress.Controller` 在 host 模式默认 `ready=false`；`Configure` 成功前 `DialContext` 走 `control` 返回 `ErrTunnelNotReady`
- `proxy.Server.dial` 没有 egress 时直接报错；解析 DNS 也必须走 `tunnelResolver`，禁止 `net.DefaultResolver`
- `stopTunnel` 先 `egress.Clear()` 再停 openvpn，避免窗口期泄漏

探测失败不是 panic：`connectAndVerify` 累计 `InitialProbeTries` 后返回 error，`connectLoop` 会 `markBlacklisted` 再试下一节点。固定节点也会写入黑名单，但下次循环不会因此跳过它。

---

## 校验边界

| 边界 | 做法 | 位置 |
|------|------|------|
| 启动配置 | `Config.Validate`：`NETWORK_MODE`、端口、非回环代理密码 ≥16、hy2 密码、host 禁 hy2 | `internal/config/config.go` |
| 路由 API | `Manager.SetRouteConfig`：`auto\|country\|fixed`，country/fixed 必填字段 | `internal/manager/manager.go` |
| OpenVPN 配置 | `ValidateOpenVPNProfile`：只允许窄指令集，`remote` 必须等于 CSV 里的公网 IPv4 | `internal/vpngate/parse.go` |
| 登录 | 常量时间比较用户名 + PBKDF2；失败计数；5 次 / 15 分钟 | `internal/webui/webui.go` |

不要在代理热路径上再做一套「宽松校验」。不可信输入（VPNGate CSV、订阅、UI JSON）只在入口校验一次。

---

## 反模式

- `panic` 处理业务错误（`embed` 资源缺失的 `panic` 仅限启动期静态资源）。
- 把 `err.Error()` 里的 OpenVPN 配置或密码写进 API。拉黑原因可以是短错误字符串，不要带 profile 正文。
- 用 200 + `{ok:false}` 表示鉴权失败；未登录 API 用 401 `unauthorized`。
- host 模式拨号失败时回退到 `net.Dial`。
