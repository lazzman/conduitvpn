package vpngate

import (
	"context"
	"net"
	"strings"
	"testing"
)

func TestParseMirrorTextExtractsAndNormalizesURLs(t *testing.T) {
	text := strings.Join([]string{
		"http://EXAMPLE.com:80/cn/ (Mirror location: Croatia)",
		"https://example.com:443/api/iphone/; http://example.com/path?x=1#fragment",
		"http://198.51.100.10:8080/cn/.",
		"https://[2001:db8::1]:443/cn/)",
	}, "\n")
	origins, issues := ParseMirrorText(text)
	want := []string{"http://example.com", "https://example.com", "http://198.51.100.10:8080", "https://[2001:db8::1]"}
	if len(origins) != len(want) {
		t.Fatalf("origins = %#v, want %#v (issues=%#v)", origins, want, issues)
	}
	for i := range want {
		if origins[i] != want[i] {
			t.Errorf("origin[%d] = %q, want %q", i, origins[i], want[i])
		}
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %#v", issues)
	}
}

func TestParseMirrorTextAcceptsUserSample(t *testing.T) {
	text := `http://150.40.105.24:38827/cn/ (Mirror location: Croatia (LOCAL Name: Hrvatska))
http://v160-251-62-107.41z4.static.cnode.io:46080/cn/ (Mirror location: Japan)
http://150.40.105.17:50406/cn/ (Mirror location: Croatia (LOCAL Name: Hrvatska))
http://150.40.105.12:29202/cn/ (Mirror location: Croatia (LOCAL Name: Hrvatska))
http://150.40.105.3:24869/cn/ (Mirror location: Croatia (LOCAL Name: Hrvatska))
http://150.40.105.9:26500/cn/ (Mirror location: Croatia (LOCAL Name: Hrvatska))`
	origins, issues := ParseMirrorText(text)
	if len(origins) != 6 {
		t.Fatalf("origins = %#v, want 6 (issues=%#v)", origins, issues)
	}
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %#v", issues)
	}
	for _, origin := range origins {
		if strings.Contains(origin, "/cn") {
			t.Errorf("path was not stripped: %q", origin)
		}
	}
}

func TestParseMirrorTextStripsAdjacentParentheticalDescription(t *testing.T) {
	origins, issues := ParseMirrorText("http://mirror.example/cn/(Mirror location: Japan)")
	if len(origins) != 1 || origins[0] != "http://mirror.example" {
		t.Fatalf("origins = %#v, issues = %#v", origins, issues)
	}
}

func TestParseMirrorTextStripsBracketDescription(t *testing.T) {
	origins, issues := ParseMirrorText("http://mirror.example/cn/[Mirror location: Japan]")
	if len(origins) != 1 || origins[0] != "http://mirror.example" || len(issues) != 0 {
		t.Fatalf("origins=%#v issues=%#v", origins, issues)
	}
}

func TestParseMirrorTextHandlesUppercaseIPv6URLWithDescription(t *testing.T) {
	origins, issues := ParseMirrorText("HTTP://[2001:db8::1]/cn/[Mirror location: Japan]")
	if len(origins) != 1 || origins[0] != "http://[2001:db8::1]" || len(issues) != 0 {
		t.Fatalf("origins=%#v issues=%#v", origins, issues)
	}
}

func TestParseMirrorTextFindsAdjacentURLsAndDeduplicates(t *testing.T) {
	origins, issues := ParseMirrorText("http://one.example/a,http://two.example/b http://one.example/c")
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
	want := []string{"http://one.example", "http://two.example"}
	if strings.Join(origins, "\n") != strings.Join(want, "\n") {
		t.Fatalf("origins = %#v, want %#v", origins, want)
	}
}

func TestParseMirrorTextDoesNotTreatQueryURLAsAnotherMirror(t *testing.T) {
	origins, issues := ParseMirrorText("http://one.example/path?next=http://two.example/api")
	if len(issues) != 0 {
		t.Fatalf("issues = %#v", issues)
	}
	if len(origins) != 1 || origins[0] != "http://one.example" {
		t.Fatalf("origins = %#v, want only the outer URL origin", origins)
	}
}

func TestParseMirrorTextHandlesSeparatorsWithoutWhitespace(t *testing.T) {
	origins, issues := ParseMirrorText("https://one.example,https://two.example;https://three.example|example.net")
	if len(origins) != 3 {
		t.Fatalf("origins = %#v, want three (issues=%#v)", origins, issues)
	}
	if len(issues) != 1 || issues[0].Token != "example.net" {
		t.Fatalf("issues = %#v, want bare host issue", issues)
	}
}

func TestParseMirrorTextReportsCredentialsAndBareAddresses(t *testing.T) {
	origins, issues := ParseMirrorText("http://user:secret@example.com/cn/ example.net:8080) 203.0.113.7 [::1] ftp://example.com")
	if len(origins) != 0 {
		t.Fatalf("origins = %#v, want none", origins)
	}
	if len(issues) < 5 {
		t.Fatalf("issues = %#v, want credential and bare-address issues", issues)
	}
	joined := make([]string, 0, len(issues))
	for _, issue := range issues {
		joined = append(joined, issue.Token+": "+issue.Reason)
	}
	all := strings.Join(joined, "\n")
	for _, want := range []string{"user:secret@example.com", "example.net:8080", "203.0.113.7", "[::1]", "example.com"} {
		if !strings.Contains(all, want) {
			t.Errorf("issues %q do not mention %q", all, want)
		}
	}
}

func TestNormalizeMirrorOrigin(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
		ok   bool
	}{
		{name: "http default port", raw: "HTTP://Example.COM:80/cn/", want: "http://example.com", ok: true},
		{name: "https default port", raw: "https://Example.COM:443/path", want: "https://example.com", ok: true},
		{name: "non default port", raw: "https://Example.COM:8443/path?x=1#f", want: "https://example.com:8443", ok: true},
		{name: "port canonicalization", raw: "http://Example.COM:00081/path", want: "http://example.com:81", ok: true},
		{name: "ipv4", raw: "http://8.8.8.8:8080/", want: "http://8.8.8.8:8080", ok: true},
		{name: "ipv6", raw: "https://[2001:4860:4860::8888]/", want: "https://[2001:4860:4860::8888]", ok: true},
		{name: "ipv6 no path", raw: "https://[2001:4860:4860::8888]", want: "https://[2001:4860:4860::8888]", ok: true},
		{name: "credentials", raw: "http://user:pass@example.com", ok: false},
		{name: "bare", raw: "example.com:8080", ok: false},
		{name: "bad scheme", raw: "ftp://example.com", ok: false},
		{name: "bad port", raw: "http://example.com:65536", ok: false},
		{name: "zone", raw: "http://[fe80::1%25en0]", ok: false},
		{name: "numeric IPv4 spelling", raw: "http://0177.0.0.1", ok: false},
		{name: "numeric IPv4 invalid", raw: "http://999.1.1.1", ok: false},
		{name: "numeric host", raw: "http://2130706433", ok: false},
		{name: "short numeric IPv4", raw: "http://127.1", ok: false},
		{name: "hex numeric host", raw: "http://0x7f000001", ok: false},
		{name: "hex-like domain", raw: "http://x.af", want: "http://x.af", ok: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeMirrorOrigin(tt.raw)
			if tt.ok {
				if err != nil || got != tt.want {
					t.Fatalf("NormalizeMirrorOrigin(%q) = %q, %v; want %q", tt.raw, got, err, tt.want)
				}
			} else if err == nil {
				t.Fatalf("NormalizeMirrorOrigin(%q) = %q, want error", tt.raw, got)
			}
		})
	}
}

func TestParseMirrorTextTrimsTrailingColonPunctuation(t *testing.T) {
	origins, issues := ParseMirrorText("http://example.com:")
	if len(origins) != 1 || origins[0] != "http://example.com" || len(issues) != 0 {
		t.Fatalf("origins=%#v issues=%#v", origins, issues)
	}
	for _, suffix := range []string{"`", "’", "”", "〉"} {
		origins, issues = ParseMirrorText("http://example.com" + suffix)
		if len(origins) != 1 || origins[0] != "http://example.com" || len(issues) != 0 {
			t.Fatalf("suffix %q: origins=%#v issues=%#v", suffix, origins, issues)
		}
	}
}

func TestValidateMirrorOriginRejectsNonPublicTargets(t *testing.T) {
	ctx := context.Background()
	for _, raw := range []string{
		"http://127.0.0.1",
		"http://10.0.0.1",
		"http://172.16.0.1",
		"http://192.168.1.1",
		"http://169.254.1.1",
		"http://100.64.0.1",
		"http://192.0.2.1",
		"http://198.51.100.1",
		"http://203.0.113.1",
		"http://[::1]",
		"http://[fc00::1]",
		"http://[2001:db8::1]",
		"ftp://8.8.8.8",
		"http://user:pass@8.8.8.8",
	} {
		if err := ValidateMirrorOrigin(ctx, raw); err == nil {
			t.Errorf("ValidateMirrorOrigin(%q) accepted a non-public or invalid target", raw)
		}
	}
	if err := ValidateMirrorOrigin(ctx, "http://8.8.8.8"); err != nil {
		t.Fatalf("public IPv4 rejected: %v", err)
	}
	if err := ValidateMirrorOrigin(ctx, "https://[2001:4860:4860::8888]"); err != nil {
		t.Fatalf("public IPv6 rejected: %v", err)
	}
}

func TestIsPublicMirrorIPRejectsSpecialRanges(t *testing.T) {
	for _, raw := range []string{
		"0.0.0.1", "192.31.196.1", "192.88.99.1", "192.175.48.1",
		"198.18.0.1", "224.0.0.1", "255.255.255.255",
		"::c000:201", "2001:1::1",
	} {
		ip := net.ParseIP(raw)
		if ip == nil || isPublicMirrorIP(ip) {
			t.Errorf("isPublicMirrorIP(%s) = true, want false", raw)
		}
	}
}
