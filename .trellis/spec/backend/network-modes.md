# 网络模式

> 两种互斥的路由策略。改拨号、hy2、netfix 或 OpenVPN 参数前先读这个文件。

---

## 选择

| | `NETWORK_MODE=container`（方案 B） | `NETWORK_MODE=host`（方案 A） |
|--|--|--|
| 场景 | Docker 生产，`NET_ADMIN` + `/dev/net/tun` | Linux/macOS 直接跑 |
| OpenVPN | 接受 `redirect-gateway`（不要 `--route-nopull`） | `--route-nopull`，宿主机默认路由不变 |
| 入站回包 | `netfix.Apply` 用 connmark 把 UI/代理/hy2 回包送回 docker 网桥 | 不需要 netfix |
| 应用出站 | 命名空间默认路由已经是 tun | `egress.Controller` 把 socket bind/mark 到 tun 设备 |
| hy2 | 可选 | `Validate` 拒绝 `HY2_PORT != 0` |
| 就绪默认 | `egress` 一开始就是 ready | 握手成功且 `Configure(device)` 之前 fail-closed |

生产启动必须显式设置模式。`--demo` 例外，不拉隧道。

---

## 方案 B：container + netfix

`redirect-gateway` 之后，容器默认路由是 tun0，入站服务的回包也会进隧道，客户端收不到响应。`internal/netfix/netfix.go`：

- 对 UI 端口、代理端口（TCP）和可选 hy2 端口（UDP）打 `CONNMARK 0x1`
- `OUTPUT` 把 marked 连接标到 fwmark
- `ip rule` / table `101` 经 docker 网关回去

`Apply` 是幂等的（先 `-C` 再 `-A`）。失败只 `logx.Warn`，进程继续——入站会降级，出站代理仍走隧道。不要把 netfix 失败当成致命错误。

hy2 只在 container 装配：`hy2.Start` 写自签证书 + sing-box hysteria2 inbound，outbound 为 `direct`（吃容器默认路由）。

---

## 方案 A：host + egress

`internal/egress`：

- `New("host")` → `ready=false`
- 握手后 `Configure(device)`：设备必须存在，然后 `setupHostRoute`（Linux 策略路由 + mark，macOS bind 到 utun）
- `Clear()` 立即把 ready 置假并拆策略，在 `tunnel.Stop` 之前调用
- `DialContext` / `Resolver` 共用这条策略；DNS 不得走系统 stub

平台文件：`egress_linux.go`、`egress_darwin.go`、`egress_unsupported.go`。新的 host 行为必须保持「未就绪则错」；见 `egress_test.go` 的 `TestHostModeFailsClosedBeforeTunnel`。

黑名单隔离验证用 `NewDeviceDialer`，不要去动正在服务的 `Controller`。

---

## 代理与探测

- `proxy.Server` 按首字节分流：`0x05` → SOCKS5，否则 HTTP。
- 所有上游 TCP 和 DNS 走 `egress`。IPv4 only（`no IPv4 address` 则失败）。
- `health.Prober` 请求 `https://<硬编码IP>/generate_204`，`InsecureSkipVerify` 是刻意的（可达性，不是认证）。不要改成需要 DNS 的主机名。

隧道未就绪时代理应 502 / 断开，而不是把请求发到宿主机网卡。

---

## 反模式

- 在 host 模式开 hy2「先试试看」
- 给 OpenVPN 同时加 `redirect-gateway` 和 `--route-nopull`
- 用 `net.Dial` / `http.DefaultClient` 做健康检查或代理转发
- 改 netfix mark/table 却不检查是否与 `egress` Linux mark 冲突
- 假设 IPv6 能通
