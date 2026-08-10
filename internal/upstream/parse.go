// Package upstream turns proxy URIs (vmess/vless/trojan/ss/hysteria2)
// and subscriptions into a sing-box config, runs sing-box as a local
// SOCKS5 gateway, and hands the effective upstream proxy back to the
// node fetcher.
package upstream

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// parseNodeURI converts a proxy URI into a sing-box outbound object.
func parseNodeURI(uri string) (map[string]any, error) {
	switch {
	case strings.HasPrefix(uri, "ss://"):
		return parseSS(uri)
	case strings.HasPrefix(uri, "vmess://"):
		return parseVMess(uri)
	case strings.HasPrefix(uri, "vless://"):
		return parseVLess(uri)
	case strings.HasPrefix(uri, "trojan://"):
		return parseTrojan(uri)
	case strings.HasPrefix(uri, "hysteria2://"), strings.HasPrefix(uri, "hy2://"):
		return parseHysteria2(uri)
	default:
		return nil, fmt.Errorf("unsupported proxy scheme in %q", truncate(uri, 48))
	}
}

// --- helpers ---

func b64Decode(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if p := len(s) % 4; p != 0 {
		s += strings.Repeat("=", 4-p)
	}
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding,
		base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("invalid base64")
}

// splitAddrPortTag parses "host:port#tag", tolerating [v6]:port.
func splitAddrPortTag(addr string) (host string, port int, tag string, err error) {
	if i := strings.LastIndex(addr, "#"); i >= 0 {
		tag = addr[i+1:]
		addr = addr[:i]
	}
	h, p, e := net.SplitHostPort(addr)
	if e != nil {
		// maybe bare host without port
		return "", 0, "", fmt.Errorf("invalid address %q", addr)
	}
	port, err = strconv.Atoi(p)
	if err != nil {
		return "", 0, "", fmt.Errorf("invalid port %q", p)
	}
	return h, port, tag, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// queryMap flattens url.Values to its first values.
func queryMap(q url.Values) map[string]string {
	m := make(map[string]string, len(q))
	for k, v := range q {
		if len(v) > 0 {
			m[k] = v[0]
		}
	}
	return m
}

// buildTLS renders the tls block from security/sni/fp/alpn/reality keys.
func buildTLS(sec, sni, fp, alpn, pbk, sid string) map[string]any {
	switch sec {
	case "tls", "xtls":
		t := map[string]any{"enabled": true, "server_name": sni}
		if fp != "" {
			t["utls"] = map[string]any{"enabled": true, "fingerprint": fp}
		}
		if alpn != "" {
			t["alpn"] = splitComma(alpn)
		}
		return t
	case "reality":
		t := map[string]any{"enabled": true, "server_name": sni}
		if pbk != "" {
			t["reality"] = map[string]any{"enabled": true, "public_key": pbk, "short_id": sid}
		}
		return t
	case "none", "":
		return nil
	default:
		// unknown security: enable TLS optimistically
		return map[string]any{"enabled": true, "server_name": sni}
	}
}

// buildTransport renders the transport block from type/path/host/... keys.
func buildTransport(p map[string]string) map[string]any {
	switch p["type"] {
	case "ws", "websocket":
		t := map[string]any{"type": "ws", "path": orDefault(p["path"], "/")}
		if p["host"] != "" {
			t["headers"] = map[string]any{"Host": p["host"]}
		}
		return t
	case "grpc":
		return map[string]any{"type": "grpc", "service_name": orDefault(p["serviceName"], p["path"])}
	case "h2", "http":
		t := map[string]any{"type": "http", "path": orDefault(p["path"], "/")}
		if p["host"] != "" {
			t["host"] = splitComma(p["host"])
		}
		return t
	default:
		return nil // tcp
	}
}

func splitComma(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- shadowsocks ---

func parseSS(uri string) (map[string]any, error) {
	rest := strings.TrimPrefix(uri, "ss://")
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return nil, fmt.Errorf("ss: missing @")
	}
	userinfo, addr := rest[:at], rest[at+1:]
	host, port, tag, err := splitAddrPortTag(addr)
	if err != nil {
		return nil, err
	}

	var method, password string
	if raw, err := b64Decode(userinfo); err == nil {
		m, p, ok := strings.Cut(string(raw), ":")
		if !ok {
			return nil, fmt.Errorf("ss: bad userinfo")
		}
		method, password = m, p
	} else {
		m, p, ok := strings.Cut(userinfo, ":")
		if !ok {
			return nil, fmt.Errorf("ss: bad userinfo")
		}
		method, password = m, p
	}
	if dec, err := url.PathUnescape(password); err == nil {
		password = dec
	}

	return map[string]any{
		"type":        "shadowsocks",
		"tag":         orDefault(tag, "ss"),
		"server":      host,
		"server_port": port,
		"method":      method,
		"password":    password,
	}, nil
}

// --- vmess ---

type vmessJSON struct {
	V    string `json:"v"`
	PS   string `json:"ps"`
	Add  string `json:"add"`
	Port string `json:"port"`
	ID   string `json:"id"`
	AID  string `json:"aid"`
	Scy  string `json:"scy"`
	Net  string `json:"net"`
	Type string `json:"type"`
	Host string `json:"host"`
	Path string `json:"path"`
	TLS  string `json:"tls"`
	SNI  string `json:"sni"`
	FP   string `json:"fp"`
	Alpn string `json:"alpn"`
}

func parseVMess(uri string) (map[string]any, error) {
	rest := strings.TrimPrefix(uri, "vmess://")
	if raw, err := b64Decode(rest); err == nil {
		return vmessFromJSON(raw)
	}
	return vmessFromPlain(rest)
}

func vmessFromJSON(raw []byte) (map[string]any, error) {
	var c vmessJSON
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("vmess: bad json: %w", err)
	}
	if c.Add == "" || c.ID == "" {
		return nil, fmt.Errorf("vmess: missing add/id")
	}
	port, _ := strconv.Atoi(c.Port)
	aid, _ := strconv.Atoi(c.AID)
	params := map[string]string{
		"type": c.Net, "path": c.Path, "host": c.Host,
		"security": c.TLS, "sni": orDefault(c.SNI, c.Host),
		"fp": c.FP, "alpn": c.Alpn,
	}
	out := map[string]any{
		"type":        "vmess",
		"tag":         orDefault(c.PS, "vmess"),
		"server":      c.Add,
		"server_port": port,
		"uuid":        c.ID,
		"alter_id":    aid,
		"security":    orDefault(c.Scy, "auto"),
	}
	if t := buildTLS(c.TLS, params["sni"], c.FP, c.Alpn, "", ""); t != nil {
		out["tls"] = t
	}
	if tr := buildTransport(params); tr != nil {
		out["transport"] = tr
	}
	return out, nil
}

func vmessFromPlain(rest string) (map[string]any, error) {
	u, err := url.Parse("vmess://" + rest)
	if err != nil {
		return nil, err
	}
	host := u.Hostname()
	port := u.Port()
	portInt, _ := strconv.Atoi(port)
	uuid := ""
	if u.User != nil {
		uuid = u.User.Username()
	}
	q := queryMap(u.Query())
	p := map[string]string{
		"type": q["type"], "path": q["path"], "host": q["host"],
		"security": q["security"], "sni": orDefault(q["sni"], q["host"]),
		"fp": q["fp"], "alpn": q["alpn"],
	}
	out := map[string]any{
		"type":        "vmess",
		"tag":         orDefault(q["name"], orDefault(q["ps"], "vmess")),
		"server":      host,
		"server_port": portInt,
		"uuid":        uuid,
		"security":    "auto",
	}
	if t := buildTLS(q["security"], p["sni"], q["fp"], q["alpn"], "", ""); t != nil {
		out["tls"] = t
	}
	if tr := buildTransport(p); tr != nil {
		out["transport"] = tr
	}
	return out, nil
}

// --- vless ---

func parseVLess(uri string) (map[string]any, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	host := u.Hostname()
	portInt, _ := strconv.Atoi(u.Port())
	uuid := ""
	if u.User != nil {
		uuid = u.User.Username()
	}
	q := queryMap(u.Query())
	p := map[string]string{
		"type": q["type"], "path": q["path"], "host": q["host"],
		"serviceName": q["serviceName"], "mode": q["mode"], "authority": q["authority"],
	}
	out := map[string]any{
		"type":        "vless",
		"tag":         orDefault(q["name"], "vless"),
		"server":      host,
		"server_port": portInt,
		"uuid":        uuid,
	}
	if f := q["flow"]; f != "" {
		out["flow"] = f
	}
	if t := buildTLS(q["security"], orDefault(q["sni"], host), q["fp"], q["alpn"], q["pbk"], q["sid"]); t != nil {
		out["tls"] = t
	}
	if tr := buildTransport(p); tr != nil {
		out["transport"] = tr
	}
	return out, nil
}

// --- trojan ---

func parseTrojan(uri string) (map[string]any, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}
	host := u.Hostname()
	portInt, _ := strconv.Atoi(u.Port())
	password := ""
	if u.User != nil {
		password, _ = url.PathUnescape(u.User.Username())
	}
	q := queryMap(u.Query())
	out := map[string]any{
		"type":        "trojan",
		"tag":         orDefault(q["name"], "trojan"),
		"server":      host,
		"server_port": portInt,
		"password":    password,
	}
	// trojan implies TLS; an explicit sni also implies it
	sec := q["security"]
	if sec == "" && q["sni"] != "" {
		sec = "tls"
	}
	if t := buildTLS(sec, orDefault(q["sni"], host), q["fp"], q["alpn"], q["pbk"], q["sid"]); t != nil {
		out["tls"] = t
	}
	return out, nil
}

// --- hysteria2 ---

func parseHysteria2(uri string) (map[string]any, error) {
	rest := strings.TrimPrefix(uri, "hysteria2://")
	rest = strings.TrimPrefix(rest, "hy2://")
	u, err := url.Parse("hysteria2://" + rest)
	if err != nil {
		return nil, err
	}
	host := u.Hostname()
	portInt, _ := strconv.Atoi(u.Port())
	password := ""
	if u.User != nil {
		password, _ = url.PathUnescape(u.User.Username())
	}
	q := queryMap(u.Query())
	tls := map[string]any{"enabled": true, "server_name": orDefault(q["sni"], host)}
	if q["insecure"] == "1" || q["insecure"] == "true" {
		tls["insecure"] = true
	}
	return map[string]any{
		"type":        "hysteria2",
		"tag":         orDefault(q["name"], "hy2"),
		"server":      host,
		"server_port": portInt,
		"password":    password,
		"tls":         tls,
	}, nil
}
