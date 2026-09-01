# 目录结构

> 单 Go 模块。业务代码只出现在 `cmd/` 与 `internal/`。

---

## 布局

```
cmd/conduitvpn/main.go          入口：解析 flag、校验配置、装配服务
internal/
  config/                       环境变量 → 类型化 Config；Validate 在开端口之前
  node/                         节点模型与排序，不含 IO
  purity/                       ipinfo 纯净度查询与机房判定
  vpngate/                      VPNGate CSV 拉取、profile 校验、上游代理拨号
  benchmark/                    并发 TCP 测速
  upstream/                     sing-box URI/订阅解析与子进程 Runner
  hy2/                          hysteria2 入站：自签证书 + 写 config + StartRunnerUDP
  tunnel/                       openvpn 子进程：握手、设备名、优雅停止
  health/                       硬编码 IP 的 HTTPS 探测（不走系统 DNS）
  egress/                       方案 A socket 绑定；host 未就绪 fail-closed
  netfix/                       方案 B 入站回包 connmark 修复
  proxy/                        单端口 HTTP+SOCKS5；DNS/TCP 走 egress
  manager/                      监督状态机（单协程循环）
  state/                        JSON 原子落盘与 UI 凭据
  logx/                         JSON 日志 + 环形缓冲 + SSE 订阅
  webui/                        REST + SSE + embed 静态资源
internal/webui/static/          原生 HTML/JS/CSS（规范见 frontend/）
```

`Dockerfile` 多阶段编译静态二进制，并打包 openvpn、iproute2、iptables、sing-box。不要在 Go 源码里 `import` 这些外部程序，一律 `os/exec`。

---

## 包职责

| 包 | 拥有 | 不要放进去 |
|----|------|------------|
| `cmd/conduitvpn` | 装配、信号处理、`--demo` / `--data-dir` | 业务循环、HTTP 路由细节 |
| `config` | env 解析与启动校验 | 运行时状态 |
| `manager` | 隧道生命周期、选路、黑名单、测速调度 | HTTP 编码、前端文案 |
| `webui` | HTTP 路由、会话、脱敏、静态资源 | 开隧道、改系统路由 |
| `state` | 文件路径、原子写、PBKDF2 | 业务决策 |
| `tunnel` / `upstream` | 子进程抽象 | 选路策略 |
| `egress` | 拨号策略与 host 路由 | OpenVPN 握手解析 |
| `proxy` | 入站协议与转发 | 决定何时隧道就绪（问 `egress.Controller`） |
| `node` | 纯数据与排序 | 网络、文件 |
| `purity` | ipinfo 查询、机房判定、纯净度记录 | 选路决策、HTTP 编码 |

新增能力时优先扩展现有包。只有当所有权无法归入上表时才新建 `internal/<pkg>`。

---

## 装配顺序

`cmd/conduitvpn/main.go` 的顺序是约定，不要打乱：

1. `config.Load()` → `--demo` / `--data-dir` 覆盖 → `cfg.Validate()`
2. `logx.Init`
3. `state.SecureDir`；host 模式空目录写 `conduitvpn.env.example`
4. `store.EnsureAuthConfigured`（demo 允许短密码；生产至少 16 位）
5. 非 demo：检查 `tunnel.Version()`；container 下 `netfix.Apply`，可选 `hy2.Start`
6. `manager.New` 或 `manager.NewDemo`；非 demo 再 `proxy.New(...).Start()`
7. `webui.New(...).Start()`
8. 非 demo 阻塞在 `m.Run(ctx)`；demo 只等信号

`manager.NewDemo` 不得启动上游、隧道、测速或健康探测。

---

## 命名

- 包名短、小写，与目录同名。
- 导出类型用职责名：`Manager`、`Store`、`Controller`、`Prober`、`Runner`。
- JSON 字段用 `snake_case`，与现有 API 对齐（`host_name`、`latency_ms`、`route_mode`）。
- 测试文件与实现同包：`internal/foo/foo_test.go`。

---

## 反模式

- 在 `internal/` 之外再放一份 Go 业务代码。
- 把 Web 框架式的 `handlers/`、`services/`、`repository/` 目录引进来。
- 让 `webui` 直接 `exec` openvpn，或让 `tunnel` 去写 `ui_auth.json`。
- 为前端再开一个 npm 工程；静态文件必须留在 `internal/webui/static/` 并被 embed。
