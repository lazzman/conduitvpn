# ConduitVPN

一个高性能的 VPNGate 代理网关管理器，用 Go 编写，**零第三方依赖**（纯标准库）。

它把 VPNGate 的公共 OpenVPN 节点变成一个**常驻的本地代理网关**：自动拉取节点、并发测速、智能选路、健康监测、故障自动漂移，并提供带 Web 管理后台的 HTTP/SOCKS5 双协议代理。

> 原项目为 Python 版 aimili-vpngate（GPL-3.0）。本仓库是独立的 Go 重写版。

---

## ✨ 功能特性

| 功能 | 说明 |
| --- | --- |
| **节点管理** | 自动拉取 VPNGate 节点列表，CSV 解析 + OpenVPN 配置解码，并发测速排序 |
| **三种路由模式** | 智能自动（失效自动漂移）/ 固定国家（如 JP、KR、US）/ 固定 IP（锁定单个节点），Web UI 或 API 运行时切换 |
| **自动漂移** | 当前节点失效后数秒内自动切换至备用健康节点；固定模式则持续重试锁定节点 |
| **双协议代理** | 单端口（默认 7928）同时支持 HTTP 与 SOCKS5，首字节嗅探自动识别 |
| **健康探测** | 硬编码 IP 的 HTTPS 探测（默认 `8.8.8.8:443`，零 DNS 依赖），连续 3 次失败触发漂移 |
| **实时延迟监控** | 后台每 10s 实测当前节点延迟，Web UI 折线图实时展示变化 |
| **Web 管理后台** | 安全后缀 URL、双主题（light/dark）、SSE 实时日志流、节点表格、状态卡片 |
| **代理鉴权** | 可选 HTTP Basic / SOCKS5 (RFC 1929) 用户名密码 |
| **隧道内 DNS** | 出站域名解析走固定公共 DNS（默认 8.8.8.8），经 VPN 出口，不依赖容器 resolv.conf |
| **上游代理拉取** | VPNGate API 被墙/污染时，可经 HTTP/SOCKS5 上游代理拉取节点（支持 Basic 认证） |
| **sing-box 上游** | 支持 vmess/vless/trojan/ss/hysteria2 等协议的**单个代理 URI** 或**订阅链接**，由内置 sing-box 引擎起本地网关 |
| **单静态二进制** | `CGO_ENABLED=0` 交叉编译，Docker 镜像 ~21MB，含 openvpn 之外的运行时依赖全内置 |

---

## 🏗️ 架构

### 路由模型：方案 B（全隧道）

- openvpn 使用服务端推送的 `redirect-gateway`，容器默认路由走 `tun0`（VPN）
- 相比 Python 原版的方案 A，**砍掉了三个子系统**：策略路由（`ip rule`/table）、`SO_BINDTODEVICE`、tun0 自定义 DNS
- **netfix 回包修复**：入站连接（Web UI/代理监听）的响应通过 connmark 标记走 docker 网关（eth0），出站代理流量仍走 VPN——解决了全隧道下外部客户端收不到回包的问题

### 状态机

```
IDLE → FETCHING(拉取+测速) → CONNECTING(拉起 openvpn) → CONNECTED(tun0 up)
     → PROBING(HTTPS 探测通过) → STABLE ──漂移──→ CONNECTING(下一个)
                                 ↑                      ↓
                                 └── 探测连败≥3 / openvpn 退出 / 模式变更 ──┘
```

- 单监督协程独占隧道生命周期，节点刷新与代理连接各自独立，channel 通信
- 黑名单持久化（`blacklist.json`），固定模式节点豁免黑名单

### 模块布局

```
cmd/conduitvpn/          入口：netfix + proxy + webui + manager 装配
internal/
├── vpngate/             API 拉取 + CSV 解析 + base64 配置解码
├── benchmark/           并发测速池（TCP connect）
├── tunnel/              openvpn 子进程管理（握手解析/优雅停止/进程回收）
├── health/              HTTPS 探测（硬编码 IP，零 DNS 依赖）
├── manager/             监督状态机 + 路由模式筛选 + 实时延迟测量
├── proxy/               单端口双协议（HTTP+SOCKS5 嗅探/中继）+ 隧道内 DNS
├── netfix/              方案 B 回包路由修复（connmark）
├── state/               JSON 原子持久化（节点/黑名单/路由/UI secret）
├── logx/                分级 JSON 日志 + 环形缓冲 + SSE 订阅
└── webui/               REST + SSE + 安全后缀 + embed 静态资源
```

---

## 🚀 快速开始

### 方式一：一键部署（Docker）

```bash
bash install.sh
# 可选参数
UI_PORT=8787 PROXY_PORT=7928 DATA_DIR=/data/conduitvpn bash install.sh
```

脚本会：构建多阶段镜像 → 以 `NET_ADMIN + /dev/net/tun` 启动容器 → 打印后台地址。

### 方式二：手动运行

```bash
docker build -t conduitvpn:latest .
docker run -d --name conduitvpn \
  --restart unless-stopped \
  --cap-add=NET_ADMIN --device=/dev/net/tun \
  -v /data/conduitvpn:/data/conduitvpn \
  -p 0.0.0.0:8787:8787 \
  -p 127.0.0.1:7928:7928 \
  conduitvpn:latest
```

### 本机开发（需 Go 1.22+）

```bash
# 无本机 Go 时可用容器编译
docker run --rm -v $PWD:/src -w /src golang:1.22-alpine \
  sh -c "CGO_ENABLED=0 go build -o /src/conduitvpn ./cmd/conduitvpn"

# 运行（需要 root + openvpn + TUN 环境）
CONDUIT_DATA_DIR=/tmp/conduitvpn ./conduitvpn
```

---

## 🖥️ 使用指南

### Web 管理后台

打开 `http://<你的IP>:8787/`（自动跳转到带安全后缀的地址，如 `/8f3a2c…`）。
后缀为随机 24 位十六进制，持久化在数据目录 `ui_auth.json`，重启不变。

页面功能：

- **状态卡片**：当前节点 / 隧道出口 / 代理端口 / 运行时长
- **路由模式**：智能 / 固定国家 / 固定 IP（下拉选择，节点表可一键「锁定」）
- **实时延迟**：当前节点延迟折线图（每 10s 实测）
- **节点表**：延迟/国家/主机/IP/协议/分数排序、过滤
- **日志流**：SSE 实时滚动，级别过滤，明暗主题切换

### 本地代理

```bash
# 终端环境
export http_proxy="http://127.0.0.1:7928"
export https_proxy="http://127.0.0.1:7928"

# Python
proxies = {"http": "http://127.0.0.1:7928", "https": "http://127.0.0.1:7928"}

# SOCKS5
curl --socks5 127.0.0.1:7928 https://api.ipify.org
```

代理端口默认仅绑定宿主机回环（`-p 127.0.0.1:7928:7928`），不对外暴露。

### 路由模式 API

```bash
# 查看当前模式
curl http://127.0.0.1:8787/<secret>/api/route

# 切换固定国家（JP/KR 等多选）
curl -X PUT -H 'Content-Type: application/json' \
  -d '{"mode":"country","country":"JP,KR"}' \
  http://127.0.0.1:8787/<secret>/api/route

# 切换固定 IP（hostname 或 IP）
curl -X PUT -H 'Content-Type: application/json' \
  -d '{"mode":"fixed","node":"vpn104003570"}' \
  http://127.0.0.1:8787/<secret>/api/route

# 切回智能自动
curl -X PUT -H 'Content-Type: application/json' \
  -d '{"mode":"auto"}' \
  http://127.0.0.1:8787/<secret>/api/route
```

---

## ⚙️ 配置（环境变量）

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `CONDUIT_DATA_DIR` | `/data/conduitvpn` | 数据目录 |
| `LOG_LEVEL` | `info` | debug / info / warn / error |
| **节点获取** | | |
| `VPNGATE_API_URL` | `https://www.vpngate.net/api/iphone/` | 节点源 |
| `FETCH_TIMEOUT_SECONDS` | `30` | 拉取超时 |
| `MAX_SCAN_ROWS` | `300` | 测速窗口上限 |
| `BENCH_CONCURRENCY` | `50` | 并发测速数 |
| `BENCH_TIMEOUT_SECONDS` | `10` | 单节点测速超时 |
| **隧道 / 健康** | | |
| `CONNECT_TIMEOUT_SECONDS` | `40` | openvpn 握手超时 |
| `PROBE_SETTLE_SECONDS` | `2` | 握手后探测缓冲 |
| `PROBE_INTERVAL_SECONDS` | `5` | 健康探测间隔 |
| `HEALTH_MAX_FAILS` | `3` | 连续失败触发漂移 |
| `HEALTH_ADDR` | `8.8.8.8:443` | 探测目标（硬编码 IP） |
| `LATENCY_INTERVAL_SECONDS` | `10` | 实时延迟测量间隔 |
| `OPENVPN_AUTH_USER/PASS` | `vpn/vpn` | VPNGate 公共凭据 |
| **路由模式** | | |
| `ROUTE_MODE` | `auto` | auto / country / fixed |
| `ROUTE_COUNTRY` | 空 | 固定国家码（如 `JP,KR`） |
| `ROUTE_NODE` | 空 | 固定节点（hostname 或 IP） |
| **代理** | | |
| `LOCAL_PROXY_HOST` | `0.0.0.0` | 代理监听地址（容器内） |
| `LOCAL_PROXY_PORT` | `7928` | 代理端口（HTTP+SOCKS5） |
| `LOCAL_PROXY_USER/PASS` | 空 | 代理鉴权（可选） |
| `DNS_SERVER` | `8.8.8.8` | 隧道内 DNS |
| `LOCAL_PROXY_MAX_CONNECTIONS` | `512` | 最大并发连接 |
| **上游代理（节点拉取，可选）** | |
| `OPENVPN_UPSTREAM_SOCKS` | 空 | SOCKS5 代理，值可为 `socks5://user:pass@host:port` 或 `host:port` |
| `OPENVPN_UPSTREAM_HTTP` | 空 | HTTP 代理，同上格式 |
| `BO_HTTP_PROXY` | 空 | 本地 HTTP 代理（配合 `BO_USER`/`BO_PASSWORD`） |
| `BO_USER` / `BO_PASSWORD` | 空 | BO 代理认证凭据 |
| `OPENVPN_UPSTREAM_USER/PASS` | 空 | 上游代理认证（URL 未带时使用） |
| `UPSTREAM_SINGBOX_URI` | 空 | 单个代理 URI：`vmess://` `vless://` `trojan://` `ss://` `hy2://`，优先级最高 |
| `UPSTREAM_SUBSCRIPTION` | 空 | 订阅链接（v2ray base64 / 纯文本 URI / sing-box JSON） |
| `UPSTREAM_SINGBOX_INDEX` | `0` | 订阅节点序号（可负值取倒数） |
| `UPSTREAM_SINGBOX_CONFIG` | 空 | sing-box 完整配置（路径或内联 JSON，需含 socks inbound） |
| `UPSTREAM_SINGBOX_PORT` | `10800` | 本地 socks 监听端口 |
| **Web UI** | | |
| `UI_HOST` / `UI_PORT` | `0.0.0.0:8787` | 管理后台 |

---

## 🔌 API 一览

| 端点（前缀 `/<secret>`） | 方法 | 说明 |
| --- | --- | --- |
| `/api/state` | GET | 实时状态快照（状态/当前节点/路由/延迟） |
| `/api/nodes` | GET | 节点列表 |
| `/api/route` | GET/PUT | 路由模式读写 |
| `/api/blacklist` | GET | 黑名单 |
| `/api/logs` | GET | 最近日志（`?n=` 条数） |
| `/api/logs/stream` | GET | SSE 实时日志 + 状态流 |
| `/api/actions/update-nodes` | POST | 立即重新拉取测速 |
| `/healthz` | GET | 健康检查（无鉴权，供 Docker HEALTHCHECK） |

> 响应中的节点数据已脱敏，不含 OpenVPN 配置内容（证书/私钥不会泄露）。

---

## 🧰 开发

```bash
make build   # 容器内编译
make test    # 单测（tunnel/proxy/manager）
make vet     # 静态检查
```

测试覆盖：tunnel 事件分类、proxy 协议解析、manager 路由筛选/校验/持久化。

## 📁 部署目录

```
/data/conduitvpn/
├── nodes.json        # 缓存的节点列表
├── blacklist.json    # 黑名单
├── route.json        # 路由模式配置
├── ui_auth.json      # Web UI secret 路径
├── openvpn.auth      # openvpn 凭据（0600）
├── configs/          # 生成的 .ovpn 配置
└── logs/             # 日志
```

## ⚠️ 已知边界

- 黑名单目前不自动过期（重启后仍生效），后续可加 TTL
- 仅支持 IPv4 隧道（openvpn 默认）；纯 IPv6 环境未验证
- 固定模式若锁定节点持续不可用，会一直重试该节点（符合"始终锁定"语义），需要用户手动切回
- 上游代理的可用性取决于代理自身的来源 IP 白名单/网络策略（如 BO 代理仅允许特定网络访问）
- 订阅仅在启动时拉取一次；sing-box 节点可用性取决于订阅质量

---

## 📜 路线图

- [x] M1 骨架 + 拉取解析 + 测速 + 持久化
- [x] M2 tunnel（openvpn 进程 + 状态机 + HTTPS 探测 + 漂移）——真机验证
- [x] M3 proxy 单端口双协议 + 隧道内 DNS
- [x] M4 webui（REST + SSE + 安全后缀 + 品牌化前端）
- [x] M5 Dockerfile + install.sh + healthcheck
- [x] M6 三种路由模式 + 运行时切换
- [x] M7 国家/节点下拉 + 实时延迟图表

## 📄 License

GPL-3.0（承袭原 aimili-vpngate 项目）。
