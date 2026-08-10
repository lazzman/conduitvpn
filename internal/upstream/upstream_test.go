package upstream

import (
	"strings"
	"testing"
)

func TestParseSS(t *testing.T) {
	cases := []string{
		"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@1.2.3.4:8388#tag1",
		"ss://aes-256-gcm:password@1.2.3.4:8388#tag2",
		"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@[::1]:8388",
	}
	for _, uri := range cases {
		ob, err := parseNodeURI(uri)
		if err != nil {
			t.Fatalf("%s: %v", uri, err)
		}
		if ob["type"] != "shadowsocks" || ob["server_port"] != 8388 {
			t.Fatalf("%s: bad outbound %v", uri, ob)
		}
		if ob["password"] != "password" {
			t.Fatalf("%s: bad password %v", uri, ob["password"])
		}
	}
}

func TestParseVMessLegacy(t *testing.T) {
	// base64 of vmess json
	uri := "vmess://eyJ2IjoiMiIsInBzIjoiVEVTVDEiLCJhZGQiOiIxLjIuMy40IiwicG9ydCI6IjQ0MyIsImlkIjoiYWFhYS1iYmJiLWNjY2MiLCJhaWQiOiIwIiwic2N5IjoiYXV0byIsIm5ldCI6IndzIiwiaG9zdCI6Imhvc3QxLmNvbSIsInBhdGgiOiIvYXBpIiwidGxzIjoidGxzIn0="
	ob, err := parseNodeURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if ob["type"] != "vmess" || ob["server"] != "1.2.3.4" || ob["server_port"] != 443 {
		t.Fatalf("bad vmess: %v", ob)
	}
	if ob["uuid"] != "aaaa-bbbb-cccc" {
		t.Fatalf("bad uuid: %v", ob["uuid"])
	}
	tr, _ := ob["transport"].(map[string]any)
	if tr["type"] != "ws" || tr["path"] != "/api" {
		t.Fatalf("bad transport: %v", tr)
	}
	tl, _ := ob["tls"].(map[string]any)
	if tl["enabled"] != true || tl["server_name"] != "host1.com" {
		t.Fatalf("bad tls: %v", tl)
	}
}

func TestParseVLessReality(t *testing.T) {
	uri := "vless://uuid1@1.2.3.4:443?security=reality&sni=cdn.x.com&fp=chrome&pbk=publickey123&sid=abcd&type=ws&path=%2Fapi&name=REAL"
	ob, err := parseNodeURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if ob["type"] != "vless" || ob["uuid"] != "uuid1" {
		t.Fatalf("bad vless: %v", ob)
	}
	tl, _ := ob["tls"].(map[string]any)
	if tl["enabled"] != true || tl["server_name"] != "cdn.x.com" {
		t.Fatalf("bad tls: %v", tl)
	}
	rl, _ := tl["reality"].(map[string]any)
	if rl["public_key"] != "publickey123" || rl["short_id"] != "abcd" {
		t.Fatalf("bad reality: %v", rl)
	}
	tr, _ := ob["transport"].(map[string]any)
	if tr["type"] != "ws" || tr["path"] != "/api" {
		t.Fatalf("bad transport: %v", tr)
	}
}

func TestParseTrojan(t *testing.T) {
	uri := "trojan://mypass@5.6.7.8:443?sni=example.com#t"
	ob, err := parseNodeURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if ob["type"] != "trojan" || ob["password"] != "mypass" || ob["server"] != "5.6.7.8" {
		t.Fatalf("bad trojan: %v", ob)
	}
	tl, _ := ob["tls"].(map[string]any)
	if tl["enabled"] != true || tl["server_name"] != "example.com" {
		t.Fatalf("bad tls: %v", tl)
	}
}

func TestParseHysteria2(t *testing.T) {
	uri := "hy2://hy2pass@9.9.9.9:443?sni=hy.x.com&insecure=1#hy"
	ob, err := parseNodeURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if ob["type"] != "hysteria2" || ob["password"] != "hy2pass" {
		t.Fatalf("bad hy2: %v", ob)
	}
	tl, _ := ob["tls"].(map[string]any)
	if tl["insecure"] != true {
		t.Fatalf("bad tls: %v", tl)
	}
}

func TestParseSubscriptionPlain(t *testing.T) {
	payload := `# Подписка | test
vless://uuid1@1.2.3.4:443?security=tls#a
vmess://dWlk@9.9.9.9:80
trojan://p@5.6.7.8:443#c
`
	uris, objs, err := parseSubscription([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(uris) != 3 || len(objs) != 0 {
		t.Fatalf("uris=%d objs=%d", len(uris), len(objs))
	}
	ob, _, err := pickNode(uris, objs, 0)
	if err != nil {
		t.Fatal(err)
	}
	if ob["type"] != "vless" {
		t.Fatalf("picked wrong node: %v", ob)
	}
	// index out of range
	if _, _, err := pickNode(uris, objs, 5); err == nil {
		t.Fatal("expected out-of-range error")
	}
}

func TestParseSubscriptionBase64(t *testing.T) {
	// v2ray-style base64 subscription
	inner := "ss://YWVzLTI1Ni1nY206cGFzcw@1.2.3.4:8388\nvless://u@9.9.9.9:443?security=tls"
	payload := b64Encode([]byte(inner))
	uris, _, err := parseSubscription([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(uris) != 2 {
		t.Fatalf("uris=%d", len(uris))
	}
}

func TestParseSubscriptionSingBoxJSON(t *testing.T) {
	payload := `[
		{"type": "vless", "tag": "n1", "server": "1.2.3.4", "server_port": 443, "uuid": "u"},
		{"type": "direct", "tag": "direct"},
		{"type": "block", "tag": "block"}
	]`
	_, objs, err := parseSubscription([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 1 || objs[0]["tag"] != "n1" {
		t.Fatalf("objs=%v", objs)
	}
}

func TestBuildConfig(t *testing.T) {
	ob := map[string]any{"type": "shadowsocks", "tag": "ss", "server": "x", "server_port": 1, "method": "aes", "password": "p"}
	cfg := buildSingBoxConfig(ob, 10800)
	ibs := cfg["inbounds"].([]any)
	ib := ibs[0].(map[string]any)
	if ib["listen_port"] != 10800 || ib["type"] != "socks" {
		t.Fatalf("bad inbound: %v", ib)
	}
	obs := cfg["outbounds"].([]any)
	if len(obs) != 2 {
		t.Fatalf("outbounds: %v", obs)
	}
}

func b64Encode(b []byte) string {
	enc := strings.NewReplacer().Replace("")
	_ = enc
	const std = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var sb strings.Builder
	for _, chunk := range chunkBytes(b, 3) {
		n := (uint32(chunk[0]) << 16)
		if len(chunk) > 1 {
			n |= uint32(chunk[1]) << 8
		}
		if len(chunk) > 2 {
			n |= uint32(chunk[2])
		}
		sb.WriteByte(std[(n>>18)&63])
		sb.WriteByte(std[(n>>12)&63])
		if len(chunk) > 1 {
			sb.WriteByte(std[(n>>6)&63])
		} else {
			sb.WriteByte('=')
		}
		if len(chunk) > 2 {
			sb.WriteByte(std[n&63])
		} else {
			sb.WriteByte('=')
		}
	}
	return sb.String()
}

func chunkBytes(b []byte, n int) [][]byte {
	var out [][]byte
	for i := 0; i < len(b); i += n {
		end := i + n
		if end > len(b) {
			end = len(b)
		}
		out = append(out, b[i:end])
	}
	return out
}
