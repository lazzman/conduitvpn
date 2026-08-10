# aimilivpn

AimiliVPN — VPNGate 代理网关管理器（Go 重写版）。

架构决策（grilling session 锁定）：

- **语言**: Go，单静态二进制（CGO_ENABLED=0）
- **运行形态**: Docker（alpine + openvpn + 二进制）
- **路由模型**: 方案 B —— openvpn `redirect-gateway` 全隧道，容器内无策略路由、无 SO_BINDTODEVICE
- **代理**: 单端口 7928 双协议（HTTP + SOCKS5，首字节嗅探）
- **健康探测**: 硬编码 IP 的 HTTPS 探测，连续 3 次失败触发节点漂移
- **Web UI**: 必须，品牌化重设计（confident dark tech），embed.FS + SSE

## M1 当前范围

一次性 CLI：拉取 VPNGate 节点 → CSV 解析 → 并发测速 → 持久化 nodes.json → 打印 Top 5。

```bash
go run ./cmd/aimilivpn
# 或指定数据目录（默认 /data/aimilivpn）
AIMILI_DATA_DIR=/tmp/aimilivpn go run ./cmd/aimilivpn
```

## 环境变量

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `AIMILI_DATA_DIR` | `/data/aimilivpn` | 数据目录 |
| `VPNGATE_API_URL` | `https://www.vpngate.net/api/iphone/` | 节点源 |
| `FETCH_TIMEOUT_SECONDS` | `30` | 拉取超时 |
| `MAX_SCAN_ROWS` | `300` | 测速窗口 |
| `BENCH_CONCURRENCY` | `50` | 并发测速数 |
| `BENCH_TIMEOUT_SECONDS` | `10` | 单节点测速超时 |
| `LOG_LEVEL` | `info` | debug/info/warn/error |
| `CONNECT_TIMEOUT_SECONDS` | `40` | openvpn 握手超时 |
| `PROBE_SETTLE_SECONDS` | `2` | 握手后探测缓冲 |
| `PROBE_INTERVAL_SECONDS` | `5` | 健康探测间隔 |
| `HEALTH_MAX_FAILS` | `3` | 连续失败触发漂移 |
| `HEALTH_ADDR` | `8.8.8.8:443` | 探测目标（硬编码 IP） |
| `OPENVPN_AUTH_USER/PASS` | `vpn/vpn` | VPNGate 公共凭据 |
| `LOCAL_PROXY_HOST` | `0.0.0.0` | 代理监听地址（容器内） |
| `LOCAL_PROXY_PORT` | `7928` | 代理端口（HTTP+SOCKS5） |
| `LOCAL_PROXY_USER/PASS` | 空 | 代理鉴权（可选） |
| `DNS_SERVER` | `8.8.8.8` | 隧道内 DNS |
| `UI_HOST` / `UI_PORT` | `0.0.0.0:8787` | 管理后台 |

## 部署

```bash
# 一键（构建镜像 + 起容器，TUN + NET_ADMIN）
bash install.sh

# 或手动
UI_PORT=8787 PROXY_PORT=7928 bash install.sh
```

## 路线图

- [x] M1 骨架 + 拉取解析 + 测速 + 持久化
- [x] M2 tunnel（openvpn 进程 + 状态机 + HTTPS 探测 + 漂移）——真机验证通过
- [x] M3 proxy 单端口双协议（HTTP + SOCKS5 首字节嗅探 + 隧道内 DNS）
- [x] M4 webui（REST + SSE + 安全后缀 + 品牌化前端）
- [x] M5 Dockerfile（多阶段）+ install.sh + healthcheck

## 已知边界

- 黑名单目前不自动过期（重启后仍生效）；后续加 TTL。
- 上游代理（socks/http 出站）暂未实现，Python 版有此能力。
