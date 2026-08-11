# AGENTS.md

本文件为 AI 编码代理提供在本仓库工作时所需的项目上下文与约束。详细文档见 `README.md`。

## 仓库概览

**ConduitVPN** — 高性能 VPNGate 代理网关管理器，Go 编写，**零第三方依赖（纯标准库）**。
将 VPNGate 公共 OpenVPN 节点变成常驻代理网关：自动拉取、并发测速、智能选路、健康监测、故障自动漂移，
对外提供 HTTP / SOCKS5 / hysteria2 多协议入口与 Web 管理后台（REST + SSE 实时监控）。

原项目为 Python 版 aimili-vpngate（GPL-3.0），本仓库为独立 Go 重写版（同样 GPL-3.0）。

## 技术栈与硬性约束

- **Go 1.22，零第三方依赖** — 新增功能一律使用标准库，禁止引入外部依赖
- 单静态二进制：`CGO_ENABLED=0 go build -trimpath -o conduitvpn ./cmd/conduitvpn`
- 运行时网络模式：Docker 使用 `NETWORK_MODE=container`（需 `NET_ADMIN` + `/dev/net/tun`）；宿主机使用 `NETWORK_MODE=host`（Linux/macOS，HTTP/SOCKS5）
- 前端为原生 JS + CSS（无框架），通过 `embed` 内嵌进二进制

## 常用命令

```bash
make build   # Docker 容器内编译（宿主机可无 Go 工具链）
make test    # 单测（8 个包）
make vet     # 静态检查
# 宿主机装有 Go 时可构建；生产直接运行必须显式选择网络模式：
go build -o conduitvpn ./cmd/conduitvpn
NETWORK_MODE=host ./conduitvpn
# 可覆盖 host 模式默认的 ./data：
NETWORK_MODE=host ./conduitvpn --data-dir /var/lib/conduitvpn
```

## 架构要点

### 双网络模式
- `container`（方案 B）：openvpn 接受 `redirect-gateway`，容器默认路由走 tun0；入站回包由 **netfix** 修复。
- `host`（方案 A）：openvpn 使用 `--route-nopull`；仅代理 TCP、DNS 和探测 socket 通过 tun，宿主机默认路由不变，hy2 不支持。

### 监督状态机
单协程独占隧道生命周期：
`IDLE → FETCHING → CONNECTING → CONNECTED → PROBING → STABLE`，
节点失效/探测连败 → 拉黑 → 漂移至下一个。

### 模块布局

- `cmd/conduitvpn/` — 入口，装配 netfix + hy2 + proxy + webui + manager
- `internal/vpngate/` — VPNGate API 拉取（支持上游代理）+ CSV 解析 + base64 解码
- `internal/upstream/` — sing-box 上游：vmess/vless/trojan/ss/hy2 URI + 订阅解析
- `internal/hy2/` — hysteria2 入站网关（自签证书 + 进程管理）
- `internal/benchmark/` — 并发测速池
- `internal/tunnel/` — openvpn 子进程管理（握手解析/优雅停止/进程回收）
- `internal/health/` — HTTPS 探测（硬编码 IP，零 DNS 依赖）
- `internal/manager/` — 监督状态机 + 路由模式筛选 + 实时延迟测量
- `internal/proxy/` — 单端口双协议（HTTP+SOCKS5）+ 隧道内 DNS
- `internal/egress/` — 宿主机方案 A 的 socket mark/接口绑定与路由清理
- `internal/netfix/` — 方案 B 回包路由修复（TCP/UDP connmark）
- `internal/state/` — JSON 原子持久化（节点/黑名单/路由/UI secret）
- `internal/logx/` — 分级 JSON 日志 + 环形缓冲 + SSE 订阅
- `internal/webui/` — REST + SSE + 安全后缀 + embed 静态资源

## 关键约定

- 数据目录：container 默认 `/data/conduitvpn`；host 默认当前 `./data`。`--data-dir` 优先于 `CONDUIT_DATA_DIR`，空 host 目录会生成无密码的 `conduitvpn.env.example` 模板
- 日志统一走 `internal/logx`，支持 `LOG_LEVEL`（debug/info/warn/error）
- API 响应的节点数据脱敏，**不含** OpenVPN 配置（证书/私钥不泄露）
- Web UI 入口为随机 24 位十六进制安全后缀，root 返回 404
- 完整环境变量表、路由模式 API 与配置说明见 README「配置」「API 一览」章节

## 已知边界（勿轻易改动）

- 黑名单不自动过期（重启后仍生效）
- 仅支持 IPv4 隧道
- 固定模式锁定节点持续不可用时一直重试（符合"始终锁定"语义）
- 订阅仅在启动时拉取一次

## 修改指南

- 改 Web UI：编辑 `internal/webui/static/`（index.html / app.js / styles.css / login.html），改后需重新编译才会生效
- 外部进程（openvpn / sing-box / hy2）统一走子进程管理抽象，修改时保持握手超时、优雅停止、进程回收的既有健壮性
- 为新增逻辑补充 `*_test.go`，维持既有测试包覆盖
- 提交信息沿用仓库中文简洁风格（如 `UI: ...`、`模块: 说明`）
- **禁止主动 git 操作**：除非用户明确要求，不得执行 `git commit`、`git push`、分支创建/合并等操作
