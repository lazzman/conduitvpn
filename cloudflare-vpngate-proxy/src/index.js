const DEFAULT_VPNGATE_API_URL = "https://www.vpngate.net/api/iphone/";
const API_PATHS = new Set(["/api/iphone", "/api/iphone/"]);
const PASSWORD_RE = /^[A-Za-z0-9_-]{32,128}$/;
const textEncoder = new TextEncoder();

export default {
  async fetch(request, env) {
    const clientURL = new URL(request.url);

    if (!API_PATHS.has(clientURL.pathname)) {
      return json({ error: "not_found" }, 404);
    }
    if (request.method !== "GET" && request.method !== "HEAD") {
      return json({ error: "method_not_allowed" }, 405, {
        Allow: "GET, HEAD",
      });
    }

    let password;
    try {
      password = getProxyPassword(env);
    } catch (error) {
      console.error("invalid VPNGATE_PROXY_PASSWORD", error);
      return json({ error: "proxy_unavailable" }, 503);
    }
    if (!hasValidPassword(request, password)) {
      return json({ error: "authentication_required" }, 401, {
        "WWW-Authenticate": 'Basic realm="VPNGate source", charset="UTF-8"',
      });
    }

    let upstreamURL;
    try {
      upstreamURL = getUpstreamURL(env);
    } catch (error) {
      console.error("invalid VPNGATE_API_URL", error);
      return json({ error: "invalid_upstream_configuration" }, 500);
    }

    // 防止将环境变量错误配置为当前 Worker，避免形成循环代理。
    if (upstreamURL.origin === clientURL.origin) {
      return json({ error: "upstream_cannot_be_this_worker" }, 500);
    }

    try {
      const upstreamResponse = await fetch(upstreamURL, {
        method: request.method,
        redirect: "manual",
        headers: {
          Accept: "text/plain, */*;q=0.8",
          // 部分旧 IIS 镜像的 gzip 响应不完整；始终请求原始 CSV。
          "Accept-Encoding": "identity",
          "User-Agent": "ConduitVPN-VPNGate-Proxy/1.0",
        },
      });

      if (upstreamResponse.status >= 300 && upstreamResponse.status < 400) {
        return json({ error: "upstream_redirect_rejected" }, 502);
      }

      const headers = new Headers(upstreamResponse.headers);
      for (const name of [
        "connection",
        "keep-alive",
        "proxy-authenticate",
        "proxy-authorization",
        "te",
        "trailer",
        "transfer-encoding",
        "upgrade",
        "set-cookie",
      ]) {
        headers.delete(name);
      }
      headers.set("Cache-Control", "no-store");
      headers.set("X-Content-Type-Options", "nosniff");

      return new Response(
        request.method === "HEAD" ? null : upstreamResponse.body,
        { status: upstreamResponse.status, headers },
      );
    } catch (error) {
      console.error("VPNGate upstream fetch failed", error);
      return json({ error: "upstream_fetch_failed" }, 502);
    }
  },
};

function getUpstreamURL(env) {
  const configured = typeof env.VPNGATE_API_URL === "string"
    ? env.VPNGATE_API_URL.trim()
    : "";
  const url = new URL(configured || DEFAULT_VPNGATE_API_URL);

  if (!url.hostname || !["http:", "https:"].includes(url.protocol)) {
    throw new Error("VPNGATE_API_URL must be a valid HTTP(S) URL");
  }
  if (url.username || url.password) {
    throw new Error("VPNGATE_API_URL must not include credentials");
  }
  return url;
}

function getProxyPassword(env) {
  const password = typeof env.VPNGATE_PROXY_PASSWORD === "string"
    ? env.VPNGATE_PROXY_PASSWORD.trim()
    : "";
  if (!PASSWORD_RE.test(password)) {
    throw new Error("VPNGATE_PROXY_PASSWORD must be 32-128 URL-safe characters");
  }
  return password;
}

// The ConduitVPN URL form https://<password>@<worker>/api/iphone/ encodes
// <password> as the HTTP Basic Auth username with an empty password.
function hasValidPassword(request, password) {
  const expected = `Basic ${btoa(`${password}:`)}`;
  return constantTimeEqual(request.headers.get("Authorization") || "", expected);
}

function constantTimeEqual(left, right) {
  const leftBytes = textEncoder.encode(left);
  const rightBytes = textEncoder.encode(right);
  let difference = leftBytes.length ^ rightBytes.length;
  const length = Math.max(leftBytes.length, rightBytes.length);
  for (let i = 0; i < length; i += 1) {
    difference |= (leftBytes[i] || 0) ^ (rightBytes[i] || 0);
  }
  return difference === 0;
}

function json(data, status, extraHeaders = {}) {
  return new Response(JSON.stringify(data), {
    status,
    headers: {
      "Content-Type": "application/json; charset=utf-8",
      ...extraHeaders,
    },
  });
}
