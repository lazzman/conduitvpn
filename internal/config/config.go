// Package config loads typed configuration from environment variables.
// Env names mirror the legacy Python version where it makes sense, so
// existing deployments keep working with the same knobs.
package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

type Config struct {
	// Demo enables the isolated UI preview behavior selected by --demo.
	Demo             bool
	DataDir          string
	APIURL           string
	FetchTimeout     time.Duration
	FetchInterval    time.Duration
	TargetValidNodes int
	MaxScanRows      int
	BenchConcurrency int
	BenchTimeout     time.Duration
	LogLevel         string

	// Upstream proxy for node fetching (compatible with the Python
	// version's env naming). nil = direct.
	UpstreamProxy *UpstreamProxy

	// sing-box based upstream (overrides UpstreamProxy when set)
	UpstreamSingboxURI    string
	UpstreamSubscription  string
	UpstreamSingboxConfig string
	UpstreamSingboxIndex  int
	UpstreamSingboxPort   int

	// hy2 inbound gateway on the proxy path (0 = disabled)
	HY2Port         int
	HY2Bind         string
	HY2Password     string
	HY2ObfsPassword string

	// Tunnel (M2)
	ConnectTimeout    time.Duration
	ProbeSettle       time.Duration
	ProbeInterval     time.Duration
	ProbeTimeout      time.Duration
	HealthMaxFails    int
	InitialProbeTries int
	HealthAddr        string
	OpenVPNAuthUser   string
	OpenVPNAuthPass   string

	// Proxy (M3)
	LocalProxyHost string
	LocalProxyPort int
	ProxyUser      string
	ProxyPass      string
	ProxyMaxConns  int
	DNSServer      string

	// Web UI (M4)
	UIHost     string
	UIPort     int
	UIUser     string
	UIPassword string
	UITLSCert  string
	UITLSKey   string

	// Route mode (M6)
	RouteMode    string
	RouteCountry string
	RouteNode    string

	// Live latency (M7)
	LatencyInterval time.Duration
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func Load() Config {
	return Config{
		DataDir:          envStr("CONDUIT_DATA_DIR", "/data/conduitvpn"),
		APIURL:           envStr("VPNGATE_API_URL", "https://www.vpngate.net/api/iphone/"),
		FetchTimeout:     time.Duration(envInt("FETCH_TIMEOUT_SECONDS", 30)) * time.Second,
		FetchInterval:    time.Duration(envInt("FETCH_INTERVAL_SECONDS", 1260)) * time.Second,
		TargetValidNodes: envInt("TARGET_VALID_NODES", 3),
		MaxScanRows:      envInt("MAX_SCAN_ROWS", 300),
		BenchConcurrency: envInt("BENCH_CONCURRENCY", 50),
		BenchTimeout:     time.Duration(envInt("BENCH_TIMEOUT_SECONDS", 10)) * time.Second,
		LogLevel:         envStr("LOG_LEVEL", "info"),

		UpstreamProxy: parseUpstreamProxy(),

		UpstreamSingboxURI:    envStr("UPSTREAM_SINGBOX_URI", ""),
		UpstreamSubscription:  envStr("UPSTREAM_SUBSCRIPTION", ""),
		UpstreamSingboxConfig: envStr("UPSTREAM_SINGBOX_CONFIG", ""),
		UpstreamSingboxIndex:  envInt("UPSTREAM_SINGBOX_INDEX", 0),
		UpstreamSingboxPort:   envInt("UPSTREAM_SINGBOX_PORT", 10800),

		HY2Port:         envInt("HY2_PORT", 0),
		HY2Bind:         envStr("HY2_BIND", "0.0.0.0"),
		HY2Password:     envStr("HY2_PASSWORD", ""),
		HY2ObfsPassword: envStr("HY2_OBFS_PASSWORD", ""),

		ConnectTimeout:    time.Duration(envInt("CONNECT_TIMEOUT_SECONDS", 40)) * time.Second,
		ProbeSettle:       time.Duration(envInt("PROBE_SETTLE_SECONDS", 2)) * time.Second,
		ProbeInterval:     time.Duration(envInt("PROBE_INTERVAL_SECONDS", 5)) * time.Second,
		ProbeTimeout:      time.Duration(envInt("PROBE_TIMEOUT_SECONDS", 5)) * time.Second,
		HealthMaxFails:    envInt("HEALTH_MAX_FAILS", 3),
		InitialProbeTries: envInt("INITIAL_PROBE_TRIES", 3),
		HealthAddr:        envStr("HEALTH_ADDR", "8.8.8.8:443"),
		OpenVPNAuthUser:   envStr("OPENVPN_AUTH_USER", "vpn"),
		OpenVPNAuthPass:   envStr("OPENVPN_AUTH_PASS", "vpn"),

		LocalProxyHost: envStr("LOCAL_PROXY_HOST", "127.0.0.1"),
		LocalProxyPort: envInt("LOCAL_PROXY_PORT", 7928),
		ProxyUser:      envStr("LOCAL_PROXY_USER", ""),
		ProxyPass:      envStr("LOCAL_PROXY_PASS", ""),
		ProxyMaxConns:  envInt("LOCAL_PROXY_MAX_CONNECTIONS", 512),
		DNSServer:      envStr("DNS_SERVER", "8.8.8.8"),

		UIHost:     envStr("UI_HOST", "127.0.0.1"),
		UIPort:     envInt("UI_PORT", 8787),
		UIUser:     envStr("UI_USER", ""),
		UIPassword: envStr("UI_PASSWORD", ""),
		UITLSCert:  envStr("UI_TLS_CERT", ""),
		UITLSKey:   envStr("UI_TLS_KEY", ""),

		RouteMode:    envStr("ROUTE_MODE", "auto"),
		RouteCountry: envStr("ROUTE_COUNTRY", ""),
		RouteNode:    envStr("ROUTE_NODE", ""),

		LatencyInterval: time.Duration(envInt("LATENCY_INTERVAL_SECONDS", 10)) * time.Second,
	}
}

// Validate rejects insecure listener and credential combinations before any
// network service starts. Existing UI credentials are handled by state.Store.
func (c Config) Validate() error {
	if err := validPort("UI_PORT", c.UIPort, false); err != nil {
		return err
	}
	if err := validPort("LOCAL_PROXY_PORT", c.LocalProxyPort, false); err != nil {
		return err
	}
	if err := validPort("HY2_PORT", c.HY2Port, true); err != nil {
		return err
	}
	if (c.UITLSCert == "") != (c.UITLSKey == "") {
		return fmt.Errorf("UI_TLS_CERT and UI_TLS_KEY must be configured together")
	}
	if !isLoopbackBind(c.LocalProxyHost) {
		if c.ProxyUser == "" || c.ProxyPass == "" {
			return fmt.Errorf("non-loopback LOCAL_PROXY_HOST requires LOCAL_PROXY_USER and LOCAL_PROXY_PASS")
		}
		if utf8.RuneCountInString(c.ProxyPass) < 16 {
			return fmt.Errorf("LOCAL_PROXY_PASS must contain at least 16 characters")
		}
	}
	if c.HY2Port != 0 && utf8.RuneCountInString(c.HY2Password) < 16 {
		return fmt.Errorf("HY2_PASSWORD must contain at least 16 characters when hy2 is enabled")
	}
	if c.ProxyMaxConns < 1 || c.ProxyMaxConns > 4096 {
		return fmt.Errorf("LOCAL_PROXY_MAX_CONNECTIONS must be between 1 and 4096")
	}
	return nil
}

func validPort(name string, port int, zeroAllowed bool) error {
	if (zeroAllowed && port == 0) || (port >= 1 && port <= 65535) {
		return nil
	}
	return fmt.Errorf("%s must be a valid TCP/UDP port", name)
}

func isLoopbackBind(host string) bool {
	host = strings.TrimSpace(strings.Trim(host, "[]"))
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DemoDataDir returns the isolated data directory used by --demo. An
// explicitly configured data directory still wins so callers can retain demo
// state between runs when needed.
func DemoDataDir() string {
	if dir := strings.TrimSpace(os.Getenv("CONDUIT_DATA_DIR")); dir != "" {
		return dir
	}
	return ".conduitvpn-demo"
}

// UpstreamProxy is an HTTP or SOCKS5 proxy used for fetching the node
// list when the VPNGate API is blocked on the direct path.
type UpstreamProxy struct {
	Type string // "http" | "socks5"
	Addr string // host:port
	User string
	Pass string
}

// parseUpstreamProxy mirrors the Python version's env precedence:
// OPENVPN_UPSTREAM_SOCKS → OPENVPN_UPSTREAM_HTTP → http(s)_proxy.
// HTTP proxies (e.g. a BO-style local proxy) go through the standard
// http_proxy/HTTP_PROXY vars with credentials embedded in the URL.
// Values may be URLs (socks5://user:pass@host:port) or bare host:port.
func parseUpstreamProxy() *UpstreamProxy {
	for _, cand := range []struct{ env, forcedType string }{
		{"OPENVPN_UPSTREAM_SOCKS", "socks5"},
		{"OPENVPN_UPSTREAM_HTTP", "http"},
		{"http_proxy", ""},
		{"HTTP_PROXY", ""},
		{"https_proxy", ""},
		{"HTTPS_PROXY", ""},
	} {
		val := strings.TrimSpace(os.Getenv(cand.env))
		if val == "" {
			continue
		}
		p, ok := parseProxyValue(val, cand.forcedType)
		if !ok {
			continue
		}
		if p.User == "" {
			p.User = firstEnv("OPENVPN_UPSTREAM_USER", "OPENVPN_UPSTREAM_USERNAME")
			p.Pass = firstEnv("OPENVPN_UPSTREAM_PASS", "OPENVPN_UPSTREAM_PASSWORD")
		}
		return p
	}
	return nil
}

func firstEnv(names ...string) string {
	for _, n := range names {
		if v := os.Getenv(n); v != "" {
			return v
		}
	}
	return ""
}

func parseProxyValue(val, forcedType string) (*UpstreamProxy, bool) {
	typ := forcedType
	var host, port, user, pass string

	if strings.Contains(val, "://") {
		u, err := url.Parse(val)
		if err != nil || u.Hostname() == "" {
			return nil, false
		}
		host, port = u.Hostname(), u.Port()
		if typ == "" {
			switch u.Scheme {
			case "socks", "socks5", "socks5h":
				typ = "socks5"
			default:
				typ = "http"
			}
		}
		if u.User != nil {
			user = u.User.Username()
			pass, _ = u.User.Password()
		}
	} else {
		if h, p, err := net.SplitHostPort(val); err == nil {
			host, port = h, p
		} else if !strings.Contains(val, ":") {
			host = val
		} else {
			return nil, false
		}
		if typ == "" {
			typ = "http"
		}
	}

	if host == "" {
		return nil, false
	}
	if port == "" {
		port = "10808" // Python-compatible default
	}
	return &UpstreamProxy{Type: typ, Addr: net.JoinHostPort(host, port), User: user, Pass: pass}, true
}
