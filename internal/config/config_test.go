package config

import "testing"

func TestParseProxyValue(t *testing.T) {
	cases := []struct {
		name       string
		val        string
		forcedType string
		wantType   string
		wantAddr   string
		wantUser   string
		wantPass   string
	}{
		{"bare host:port", "127.0.0.1:7890", "", "http", "127.0.0.1:7890", "", ""},
		{"bare host", "proxy.local", "", "http", "proxy.local:10808", "", ""},
		{"socks url", "socks5://u:p@1.2.3.4:1080", "", "socks5", "1.2.3.4:1080", "u", "p"},
		{"socks url no port", "socks5://1.2.3.4", "", "socks5", "1.2.3.4:10808", "", ""},
		{"http url", "http://5.6.7.8:3128", "", "http", "5.6.7.8:3128", "", ""},
		{"forced socks", "proxy.local:1080", "socks5", "socks5", "proxy.local:1080", "", ""},
		{"forced http overrides url scheme", "socks5://1.2.3.4:1", "http", "http", "1.2.3.4:1", "", ""},
		{"empty", "", "", "", "", "", ""},
	}
	for _, c := range cases {
		p, ok := parseProxyValue(c.val, c.forcedType)
		if c.wantAddr == "" {
			if ok {
				t.Errorf("%s: expected parse to fail, got %+v", c.name, p)
			}
			continue
		}
		if !ok {
			t.Errorf("%s: parse failed", c.name)
			continue
		}
		if p.Type != c.wantType || p.Addr != c.wantAddr || p.User != c.wantUser || p.Pass != c.wantPass {
			t.Errorf("%s: got %+v, want type=%s addr=%s user=%s pass=%s",
				c.name, p, c.wantType, c.wantAddr, c.wantUser, c.wantPass)
		}
	}
}

func TestParseUpstreamProxyPrecedence(t *testing.T) {
	t.Setenv("OPENVPN_UPSTREAM_SOCKS", "")
	t.Setenv("OPENVPN_UPSTREAM_HTTP", "")
	t.Setenv("http_proxy", "")

	t.Setenv("OPENVPN_UPSTREAM_SOCKS", "socks5://1.2.3.4:1080")
	p := parseUpstreamProxy()
	if p == nil || p.Type != "socks5" || p.Addr != "1.2.3.4:1080" {
		t.Fatalf("socks env not parsed: %+v", p)
	}

	// explicit socks should win over HTTP_PROXY
	t.Setenv("http_proxy", "http://9.9.9.9:3128")
	p = parseUpstreamProxy()
	if p == nil || p.Type != "socks5" {
		t.Fatalf("precedence broken, got %+v", p)
	}

	// fall back to HTTP_PROXY when no explicit upstream
	t.Setenv("OPENVPN_UPSTREAM_SOCKS", "")
	t.Setenv("OPENVPN_UPSTREAM_HTTP", "")
	p = parseUpstreamProxy()
	if p == nil || p.Type != "http" || p.Addr != "9.9.9.9:3128" {
		t.Fatalf("http_proxy fallback failed: %+v", p)
	}

	// explicit user/pass env for bare values
	t.Setenv("OPENVPN_UPSTREAM_HTTP", "1.2.3.4:7890")
	t.Setenv("OPENVPN_UPSTREAM_USER", "alice")
	t.Setenv("OPENVPN_UPSTREAM_PASS", "s3cret")
	p = parseUpstreamProxy()
	if p == nil || p.User != "alice" || p.Pass != "s3cret" {
		t.Fatalf("auth env not applied: %+v", p)
	}

	// nothing set → nil
	t.Setenv("OPENVPN_UPSTREAM_HTTP", "")
	t.Setenv("OPENVPN_UPSTREAM_USER", "")
	t.Setenv("http_proxy", "")
	if p := parseUpstreamProxy(); p != nil {
		t.Fatalf("expected nil proxy, got %+v", p)
	}
}

func TestParseUpstreamProxyBO(t *testing.T) {
	t.Setenv("OPENVPN_UPSTREAM_SOCKS", "")
	t.Setenv("OPENVPN_UPSTREAM_HTTP", "")
	t.Setenv("BO_HTTP_PROXY", "http://10.0.0.5:810")
	t.Setenv("BO_USER", "bob")
	t.Setenv("BO_PASSWORD", "s3cret")
	p := parseUpstreamProxy()
	if p == nil || p.Type != "http" || p.Addr != "10.0.0.5:810" {
		t.Fatalf("BO proxy not parsed: %+v", p)
	}
	if p.User != "bob" || p.Pass != "s3cret" {
		t.Fatalf("BO creds not applied: %+v", p)
	}

	// explicit OPENVPN_UPSTREAM_HTTP still wins over BO
	t.Setenv("OPENVPN_UPSTREAM_HTTP", "http://1.2.3.4:3128")
	p = parseUpstreamProxy()
	if p == nil || p.Addr != "1.2.3.4:3128" {
		t.Fatalf("precedence broken, got %+v", p)
	}
}
