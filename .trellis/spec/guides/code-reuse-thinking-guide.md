# 代码复用思考指南

> 先搜现有包，再写新函数。这个仓库很小，重复的拨号或校验会直接变成泄漏或双份协议实现。

---

## 动手前问

| 问题 | 若是 |
|------|------|
| 已经有人打过这类日志吗？ | 用 `logx`，不要 `log.Printf` |
| 已经有人拨过隧道侧 TCP/DNS 吗？ | `egress.Controller.DialContext` / `Resolver` |
| 已经有人跑过外部二进制吗？ | `tunnel.Tunnel` 或 `upstream.Runner` |
| 已经有人写过 JSON 状态吗？ | `state.writeJSON` 路径（经 Store 方法） |
| 已经有人把节点交给浏览器了吗？ | `sanitizeNode` |
| 已经有人返回过 API 错误了吗？ | `writeAPIError` + i18n `errors.*` |
| 已经有人校验过 OpenVPN 文本了吗？ | `vpngate.ValidateOpenVPNProfile` |

---

## 本仓库里的重复陷阱

### 1. 第二条出站路径

**坏**：proxy 走 egress，健康检查却 `net.Dial("tcp", "example.com:443")`。host 模式会绕过隧道，container 模式会依赖错误的 DNS。

**好**：探测用 `health.Prober`（硬编码 IP + egress dial）。代理用 `Server.dial` → `egress.DialContext`。临时验证用 `egress.NewDeviceDialer`，不要动正在服务的 controller。

### 2. 第二套子进程监督

**坏**：为 hy2 或「测速用 openvpn」再写一个 `exec` + `Wait`。

**好**：OpenVPN 用 `tunnel`，sing-box 用 `Runner`。hy2 已经是 `StartRunnerUDP`。

### 3. 第二套 JSON 写入

**坏**：在 manager 里 `os.WriteFile("blacklist.json", ...)`。

**好**：给 `state.Store` 加方法。原子 rename 和 0600 权限已经测过。

### 4. 第二套错误 JSON

**坏**：`json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})`。

**好**：稳定 `code` + `writeAPIError`。前端只认 `errors.<code>`。

### 5. 第二套节点形状

**坏**：在 webui 里为表格定义匿名 struct，字段名和 `node.Node` 的 json tag 不一致。

**好**：复用 `node.Node`（脱敏后）。前端读 `host_name`、`country_short`、`latency_ms`。

### 6. 配置常量分叉

`passwordIterations`、`sessionTTL`、netfix mark `0x1`、默认端口 8787/7928 都是单一事实来源。改一处就 `rg` 测试、Dockerfile `EXPOSE`、`.env.example`、README 表格。

---

## 何时抽取

**要抽**：第三处复制、涉及密钥/路由的逻辑、跨包行为必须一致（脱敏、fail-closed）。

**不要抽**：只用一次的 10 行、为「对称」而造的接口、把 `internal/` 变成 utils 垃圾场。

---

## 提交前

- [ ] `rg` 过函数名和错误字符串
- [ ] 没有新的 `http.Client{Timeout: ...}` 却忘了 egress（除非是 VPNGate 拉取，那走 `vpngate.Client`）
- [ ] 没有新的 `os.WriteFile` 写 data dir
- [ ] 前端没有新的重复字典
