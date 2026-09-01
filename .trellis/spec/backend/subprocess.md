# 子进程

> 外部进程：`openvpn`（隧道）和 `sing-box`（上游拉取网关 + hy2 入站）。Go 不实现这些协议，只监督进程。

---

## 共同约定

1. `exec.Command` + 独立进程组（openvpn 用 `Setpgid: true`），便于杀整个树。
2. 启动后要有就绪信号：openvpn 看握手行；sing-box TCP 探本地端口，UDP/hy2 看进程还活着。
3. 停止：SIGTERM 到进程组，宽限后 SIGKILL，等到 `Wait` 回收。
4. 把 stdout/stderr 扫进日志，但不要把配置文件内容再打一遍。
5. 配置文件写在 data dir，权限 `0600`，目录 `0700`。

不要再发明第三套监督循环。能复用 `tunnel.Tunnel` 或 `upstream.Runner` 就复用。

---

## OpenVPN：`internal/tunnel`

- `Start(Options)` 写出 `--config`、`--dev tun`、`--auth-user-pass`；host 模式加 `--route-nopull`。
- `WaitHandshake` / `WaitHandshakeContext` 等到 `Initialization Sequence Completed`，或把 AUTH/TUN/fatal/exit 变成 error。
- 从日志里解析设备名（`tun` / `utun*`），host `egress.Configure` 需要它。
- `Stop`：SIGTERM 到 `-pgid`，5 秒后 SIGKILL。
- `Version()` 在 `main` 里、开隧道之前调用；没有 openvpn 就退出。

握手分类的测试锁在 `tunnel_test.go`。改日志匹配字符串时同步改测试。

`manager.prepareFiles` 把校验过的 profile 和 auth 文件写到 data dir。写盘前再次 `ValidateOpenVPNProfile`（见 `TestPrepareFilesRevalidatesCachedProfile`）——缓存的 `nodes.json` 不能绕过校验。

---

## sing-box：`internal/upstream`

`Runner` 监督 `sing-box run -c`：

- 先 `sing-box check -c`
- `StartRunner` 等本地 SOCKS 端口；`StartRunnerUDP` 用于 hy2
- `watch()` 在意外退出后重启
- `Stop()` 取消 context 并杀掉进程

`upstream.Start` 解析有效上游，优先级：

1. `UPSTREAM_SINGBOX_URI`
2. `UPSTREAM_SUBSCRIPTION`（只在启动时 fetch）
3. `UPSTREAM_SINGBOX_CONFIG`（文件或 JSON）
4. 否则遗留 `OPENVPN_UPSTREAM_*` / `http_proxy`

URI 解析（vmess/vless/trojan/ss/hy2）在 `internal/upstream/parse.go`，测试在 `upstream_test.go`。新协议先加解析测试，再接入 `Start`。

hy2 入站（`internal/hy2`）生成 ECDSA 自签证书，写 hysteria2 inbound + direct outbound，然后 `StartRunnerUDP`。没有 `HY2_PASSWORD` 直接失败。

---

## 监督器如何使用它们

`manager.Run` 单协程：

```
fetchAndBench → selectCandidates → connectLoop
  connectAndVerify (Start + WaitHandshake + Configure + 初始探测)
  monitor (周期探测 + 实时延迟)
  失败 → markBlacklisted → 下一节点（固定节点下次仍会选中）
  耗尽 → drifting → 等待 → 再拉取
```

不要为「并行连多个 OpenVPN」再开协程。黑名单验证隧道是短命、隔离的，用 `NewDeviceDialer`，不要替换正在服务的 `m.tun`。

---

## 反模式

- `cmd.Run()` 阻塞在 openvpn 上却不解析握手
- 只 `Kill` PID 而不是进程组（子进程会成孤儿）
- 跳过 `sing-box check` 直接 `run`
- 在 Go 里实现 OpenVPN 或 hysteria2
- 把 hy2 证书写到世界可读的 `/tmp`
