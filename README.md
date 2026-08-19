# ConduitVPN

> [简体中文](README.zh-CN.md)

<p align="center">
  <img src="./assets/readme/conduitvpn-hero.svg" width="100%" alt="ConduitVPN turns public VPNGate OpenVPN nodes into a managed proxy gateway with monitored failover.">
</p>

**ConduitVPN** turns public [VPNGate](https://www.vpngate.net/) OpenVPN nodes into a managed, long-running proxy gateway. It fetches and benchmarks candidates, selects a route, monitors tunnel health, and fails over when a node becomes unusable. The single Go binary exposes HTTP, SOCKS5, an optional hysteria2 inbound gateway, and a live Web management console.

It is a clean-room Go rewrite of the GPL-3.0 Python project aimili-vpngate. The implementation uses Go 1.22 and the standard library only.

<p align="center">
  <img src="./docs/screenshots/light.png" width="100%" alt="ConduitVPN management console showing gateway state, route selection, latency, nodes, and logs.">
</p>

## Why ConduitVPN

- **Managed node lifecycle** - Fetches VPNGate nodes, benchmarks them concurrently, chooses eligible routes, and supervises OpenVPN through connection, probing, and stable states.
- **Route control** - Use automatic failover, restrict selection to one or more countries, or keep retrying one fixed node.
- **One local proxy port** - HTTP and SOCKS5 share port `7928`; DNS, TCP proxy traffic, health checks, and latency measurements use the tunnel route.
- **Two operating modes** - Full tunnel routing in Docker, or controlled socket routing on Linux and macOS hosts.
- **Observable by default** - The authenticated console provides live state, latency samples, node management, and Server-Sent Event logs.
- **Small operational surface** - A static Go binary, embedded native web UI, JSON state, and no Go module dependencies beyond the standard library.

## Quick Start

Docker Compose is the recommended production path. It uses full-tunnel container mode and requires a Linux host with Docker Compose v2 and `/dev/net/tun` available.

```bash
git clone https://github.com/sarices/conduitvpn.git
cd conduitvpn
cp .env.example .env
```

Set strong values for `UI_PASSWORD` and `LOCAL_PROXY_PASS` in `.env` (at least 16 characters each), then start the gateway:

```bash
docker compose up -d
docker logs conduitvpn
```

The startup log prints the random management path:

```text
webui listening ... path=/0123456789abcdef01234567/ auth="login required"
```

Open that path through your localhost reverse proxy or Cloudflare Tunnel. The Compose file publishes the console and HTTP/SOCKS5 proxy to host loopback only; set `HY2_PORT` and `HY2_PASSWORD` in `.env` when you want the optional public UDP hysteria2 inbound gateway.

### Local proxy

After the tunnel reaches a stable state, configure a local client to use port `7928`:

```bash
export http_proxy="http://127.0.0.1:7928"
export https_proxy="http://127.0.0.1:7928"
curl --socks5 127.0.0.1:7928 https://api.ipify.org
```

### Try the UI without a tunnel

Demo mode starts only the embedded management console with deterministic sample data. It creates no VPN tunnel and no proxy service.

```bash
go run ./cmd/conduitvpn --demo
```

By default, the demo uses `admin` / `demo` and stores its isolated data in `./.conduitvpn-demo`. Its root URL redirects to the generated management path.

## How It Works

<p align="center">
  <img src="./docs/architecture.svg" width="100%" alt="ConduitVPN production architecture for container full-tunnel and host controlled-routing modes.">
</p>

The supervisor owns one tunnel lifecycle:

```text
IDLE -> FETCHING -> CONNECTING -> CONNECTED -> PROBING -> STABLE
                   failure or route change -> blacklist -> select next node
```

| Mode | Use case | Routing behavior | Inbound protocols |
| --- | --- | --- | --- |
| `NETWORK_MODE=container` | Docker production deployment | OpenVPN accepts `redirect-gateway`; `netfix` repairs inbound reply routing with connection marks. | HTTP, SOCKS5, optional hysteria2 |
| `NETWORK_MODE=host` | Direct Linux or macOS execution | OpenVPN uses `--route-nopull`; `egress` sends only controlled sockets through the tunnel. | HTTP and SOCKS5 only |

In both modes, the host or container default route is protected from proxy leakage while the tunnel is unavailable. Host mode never changes the system default route; Linux uses socket marks and policy routing, while macOS binds controlled sockets to the OpenVPN `utun` interface.

## Security and Compatibility

- Production startup requires an explicit `NETWORK_MODE`: `container` or `host`.
- Non-loopback proxy listeners require `LOCAL_PROXY_USER` and a password of at least 16 characters. Docker deployment always requires proxy authentication.
- The management console requires `UI_USER` and a password of at least 16 characters on its first production start. Credentials are stored as salted PBKDF2 hashes; sessions use HttpOnly, SameSite=Strict cookies.
- The console is served below a random 24-hex-character path; `/` returns `404` in production. `GET /healthz` remains unauthenticated for health checks.
- Host mode is supported only on Linux and macOS, requires administrator privileges to create TUN devices, and does not support hysteria2.
- Only IPv4 tunnels are supported.

<details>
<summary><strong>Advanced deployment and configuration</strong></summary>

### Host mode

Install OpenVPN first. Linux additionally needs `iproute2`; macOS users can install OpenVPN with Homebrew. Run on an isolated host or VM for production use.

```bash
CGO_ENABLED=0 go build -trimpath -o conduitvpn ./cmd/conduitvpn

sudo env NETWORK_MODE=host \
  UI_USER=admin UI_PASSWORD='a-long-random-password' \
  LOCAL_PROXY_USER=proxy LOCAL_PROXY_PASS='another-long-random-password' \
  ./conduitvpn --data-dir /var/lib/conduitvpn
```

`--data-dir` takes precedence over `CONDUIT_DATA_DIR`. In host mode, an unset data directory defaults to `./data`; an empty directory receives a `conduitvpn.env.example` template without credentials. Set `HY2_PORT=0` in this mode.

### Manual Docker run

```bash
docker run -d --name conduitvpn \
  --restart unless-stopped \
  --cap-drop=ALL --cap-add=NET_ADMIN --security-opt=no-new-privileges \
  --read-only --tmpfs /tmp --device=/dev/net/tun \
  -v /data/conduitvpn:/data/conduitvpn \
  -p 127.0.0.1:8787:8787 \
  -p 127.0.0.1:7928:7928 \
  -e NETWORK_MODE=container \
  -e UI_HOST=0.0.0.0 -e UI_USER=admin -e UI_PASSWORD='a-long-random-password' \
  -e LOCAL_PROXY_HOST=0.0.0.0 -e LOCAL_PROXY_USER=proxy -e LOCAL_PROXY_PASS='another-long-random-password' \
  ghcr.io/sarices/conduitvpn:latest
```

Add `-p 0.0.0.0:7929:7929/udp -e HY2_PORT=7929 -e HY2_PASSWORD='a-third-long-random-password'` to enable hysteria2. Any compatible hysteria2 client can connect to the server IP and UDP port with certificate verification disabled; optional `HY2_OBFS_PASSWORD` enables salamander obfuscation.

### Configuration reference

| Group | Variables |
| --- | --- |
| Runtime | `NETWORK_MODE`, `CONDUIT_DATA_DIR`, `--data-dir`, `LOG_LEVEL` |
| Node source and benchmark | `VPNGATE_API_URL`, `FETCH_TIMEOUT_SECONDS`, `FETCH_INTERVAL_SECONDS`, `TARGET_VALID_NODES`, `MAX_SCAN_ROWS`, `BENCH_CONCURRENCY`, `BENCH_TIMEOUT_SECONDS` |
| Tunnel and health | `CONNECT_TIMEOUT_SECONDS`, `PROBE_SETTLE_SECONDS`, `PROBE_INTERVAL_SECONDS`, `PROBE_TIMEOUT_SECONDS`, `INITIAL_PROBE_TRIES`, `HEALTH_MAX_FAILS`, `HEALTH_ADDR`, `OPENVPN_AUTH_USER`, `OPENVPN_AUTH_PASS` |
| Routes | `ROUTE_MODE`, `ROUTE_COUNTRY`, `ROUTE_NODE`, `LATENCY_INTERVAL_SECONDS` |
| Local proxy | `LOCAL_PROXY_HOST`, `LOCAL_PROXY_PORT`, `LOCAL_PROXY_USER`, `LOCAL_PROXY_PASS`, `LOCAL_PROXY_MAX_CONNECTIONS`, `DNS_SERVER` |
| hysteria2 inbound | `HY2_PORT`, `HY2_BIND`, `HY2_PASSWORD`, `HY2_OBFS_PASSWORD` |
| Fetch upstream | `OPENVPN_UPSTREAM_SOCKS`, `OPENVPN_UPSTREAM_HTTP`, `OPENVPN_UPSTREAM_USER`, `OPENVPN_UPSTREAM_PASS`, `UPSTREAM_SINGBOX_URI`, `UPSTREAM_SUBSCRIPTION`, `UPSTREAM_SINGBOX_CONFIG`, `UPSTREAM_SINGBOX_INDEX`, `UPSTREAM_SINGBOX_PORT` |
| Web console | `UI_HOST`, `UI_PORT`, `UI_USER`, `UI_PASSWORD`, `UI_TLS_CERT`, `UI_TLS_KEY` |

The fetch upstream accepts HTTP/SOCKS5 proxies, a sing-box URI (`vmess://`, `vless://`, `trojan://`, `ss://`, or `hy2://`), or a v2ray-base64, plain-text, or sing-box JSON subscription. A subscription is fetched once at startup.

### Route API

All API routes are prefixed by `/<secret>`. Node responses exclude OpenVPN configuration material, including certificates and private keys.

| Endpoint | Method | Purpose |
| --- | --- | --- |
| `/api/state` | `GET` | Current gateway state |
| `/api/nodes` | `GET` | Sanitized node list |
| `/api/route` | `GET`, `PUT` | Read or set automatic, country, or fixed-node routing |
| `/api/blacklist` | `GET` | Blacklisted nodes |
| `/api/logs`, `/api/logs/stream` | `GET`, `GET` SSE | Recent logs and live log stream |
| `/api/actions/update-nodes` | `POST` | Fetch and benchmark nodes immediately |
| `/api/actions/test-blacklist` | `GET`, `POST` | Inspect or start isolated blacklist verification |
| `/api/actions/restore-available-blacklist` | `POST` | Restore verified available nodes |
| `/healthz` | `GET` | Unauthenticated health check |

```bash
# Restrict automatic selection to Japan and South Korea.
curl -X PUT -H 'Content-Type: application/json' \
  -d '{"mode":"country","country":"JP,KR"}' \
  http://127.0.0.1:8787/<secret>/api/route

# Lock a node, then return to automatic routing.
curl -X PUT -d '{"mode":"fixed","node":"vpn104003570"}' \
  http://127.0.0.1:8787/<secret>/api/route
curl -X PUT -d '{"mode":"auto"}' \
  http://127.0.0.1:8787/<secret>/api/route
```

</details>

## Development

The Makefile runs the Go toolchain in a temporary `golang:1.22-alpine` container, so a host Go installation is optional.

```bash
make build
make test
make vet
```

GitHub Actions run vet and tests on Linux and macOS, build multi-architecture container images for `linux/amd64` and `linux/arm64`, and attach static binaries to version tags.

## Known Limits

- Blacklisted nodes do not expire automatically and remain blacklisted after restart.
- A fixed node that remains unavailable is retried indefinitely, by design.
- Upstream availability depends on the source IP policy of that upstream.
- Subscriptions are fetched only once during startup.

## License

GPL-3.0, inherited from the original aimili-vpngate project.
