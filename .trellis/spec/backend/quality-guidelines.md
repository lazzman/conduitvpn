# 质量与约束

> 这是硬约束，不是风格偏好。违反它们会让二进制、镜像或安全模型失效。

---

## 零第三方 Go 依赖

`go.mod` 只有：

```
module conduitvpn
go 1.22
```

禁止 `require`。加密（PBKDF2、HMAC）、HTTP、CSV、embed、TLS 一律标准库。需要 hysteria2 / vmess 时，写 JSON 配置并 `exec` 镜像里的 `sing-box`，不要拉 Go 客户端库。

构建：

```bash
CGO_ENABLED=0 go build -trimpath -o conduitvpn ./cmd/conduitvpn
make build   # 容器内同样的静态构建
make test
make vet
```

不要为了 cgo 绑定 iptables/路由；`netfix` 和 `egress` 已经用 `ip` / `iptables` 命令。

---

## 脱敏

节点模型含 `ConfigText`（OpenVPN 配置，含证书和私钥）。离开进程前必须清掉：

```go
func sanitizeNode(n *node.Node) *node.Node {
    if n == nil {
        return nil
    }
    c := *n
    c.ConfigText = ""
    return &c
}
```

`apiState`、SSE `state` 与 `GET /api/nodes` 都走 `sanitizeNode`（节点列表再合并 `purity`）。磁盘上的 `nodes.json` 仍保留 `config_text` 以便重连。不要把 profile 写进 API、日志或错误信息。

VPNGate 配置在解析时就必须过 `ValidateOpenVPNProfile`：禁止 script/plugin/管理口，且 `remote` 必须等于该行广告的公网 IPv4。见 `internal/vpngate/parse.go` 与 `profile_test.go`。

---

## 测试

风格：标准库 `testing`，表驱动或逐用例 `t.Fatal`。无 testify。

可信范例：

| 主题 | 文件 |
|------|------|
| 启动安全边界 | `internal/config/config_test.go` |
| 原子文件权限 / PBKDF2 | `internal/state/store_test.go` |
| 选路与黑名单验证 | `internal/manager/manager_test.go` |
| 握手分类 / `--route-nopull` | `internal/tunnel/tunnel_test.go` |
| API code、CSP、禁止 innerHTML | `internal/webui/webui_test.go` |
| URI / 订阅解析 | `internal/upstream/upstream_test.go` |
| host fail-closed | `internal/egress/egress_test.go` |

新逻辑加在同包 `*_test.go`。涉及 embed 的 UI 字符串（港澳台命名、i18n key、`innerHTML`）用读取 `staticFS` 的测试锁住，不要只靠手工点页面。

网络/权限类测试保持可在无 root、无 tun 的 `make test` 里跑。需要特权的行为用命令序列单测（如 `egress_linux_test.go` 的 `TestHostRouteCommands`），不要在 CI 里真开隧道。

---

## 已知边界（不要「顺手修」）

这些是产品语义，不是 bug：

- 黑名单不自动过期，重启仍在
- 仅 IPv4 隧道；proxy 解析只取 A 记录
- 固定模式锁定节点失败时 `markBlacklisted` 仍会写入 `blacklist.json`，但 `connectLoop` 对 `isFixedNode` 不会因拉黑而跳过，耗尽后 `drifting` 再重试（符合「始终锁定」）
- 订阅只在启动时拉一次（`upstream.Start`）
- host 模式不支持 hy2（`Validate` 拒绝 `HY2_PORT != 0`）
- 监督状态以代码为准：`idle` / `fetching` / `connecting` / `connected` / `drifting`。README 里的 PROBING/STABLE 是叙事，不要在代码或 UI 里新增这两个 state 字符串

---

## 安全默认

- 生产必须显式 `NETWORK_MODE=container|host`
- 非回环代理必须有用户名和 ≥16 位密码；Docker 绑定 `0.0.0.0` 因此始终要认证
- 首次生产启动要 `UI_USER` + ≥16 位 `UI_PASSWORD`；哈希用 600000 次 PBKDF2-SHA256
- 控制台在 24 位 hex 路径下；生产 `/` 返回 404；`GET /healthz` 无认证
- 会话 Cookie：`HttpOnly`、`SameSite=Strict`；无 TLS 名为 `conduitvpn_session`，TLS 时为 `__Secure-conduitvpn-session`
- 安全头：CSP `default-src 'self'`、`X-Frame-Options: DENY`、`nosniff`

参考 `Config.Validate`、`state.EnsureAuthConfigured`、`webui.securityHeaders`。

---

## 反模式

- 为了方便加一个 Go module（即使是 `golang.org/x/...`）
- 用 `innerHTML` 渲染节点名或日志（前端测试会失败）
- 把「演示密码 demo」写进生产 `EnsureAuthConfigured`
- 在热路径上做 VPNGate 全量 CSV 的同步解析而不走现有 `Parse` + `MaxScanRows`
