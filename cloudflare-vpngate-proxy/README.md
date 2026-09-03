# VPNGate Cloudflare Worker Proxy

该 Worker 固定反向代理 VPNGate CSV 接口，不会转发客户端给出的目标地址，因此不是开放代理。
它只接受 `/api/iphone` 与 `/api/iphone/`，并要求 HTTP Basic Auth。Worker 名称使用随机
域名前缀以降低被随意扫描的概率；密码才是实际的访问控制。

默认上游：

```text
https://www.vpngate.net/api/iphone/
```

可在 Cloudflare Worker 的 **Settings → Variables and Secrets** 中设置普通文本变量
`VPNGATE_API_URL`，使用完整 HTTP(S) URL 覆盖默认上游。

首次部署时生成随机 Worker 名称与 URL 安全的密码。实际名称和密码仅保存在自己的
密码管理器或部署配置中，不要提交到仓库：

```bash
cd cloudflare-vpngate-proxy
WORKER_NAME="vp-$(openssl rand -hex 12)"
PASSWORD="$(openssl rand -hex 32)"

# 尚未设置密码的 Worker 只会返回 503，不会开放代理。
wrangler deploy --name "$WORKER_NAME" --keep-vars
printf '%s' "$PASSWORD" | wrangler secret put VPNGATE_PROXY_PASSWORD --name "$WORKER_NAME"
```

部署后，将 ConduitVPN 的 `VPNGATE_API_URL` 设置为：

```text
https://<password>@vpngate-proxy.example.workers.dev/api/iphone/
```

其中 `<password>`、`vpngate-proxy` 和 `example` 都是占位符，必须替换为自己的值。
URL 中的 `<password>` 会作为 HTTP Basic Auth 的用户名发送，密码字段为空；生成的密码
限定为 32–128 个 URL 安全字符。该 URL 既可设置为 `VPNGATE_API_URL`，也可直接粘贴到
ConduitVPN 管理后台的 **VPNGate 镜像**，作为带密码的备用节点源。ConduitVPN 会从来源状态
和日志中剥离 URL userinfo，避免显示该密码。若需要更严格的访问控制，可额外使用 Cloudflare
Access 或 WAF。
