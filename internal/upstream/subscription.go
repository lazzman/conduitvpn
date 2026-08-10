package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// fetchSubscription downloads a subscription payload.
func fetchSubscription(ctx context.Context, rawURL string, timeout time.Duration) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 conduitvpn/0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("subscription status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 16<<20))
}

// parseSubscription turns a subscription payload into either a list of
// proxy URIs or a list of sing-box outbound objects.
func parseSubscription(raw []byte) (uris []string, outbounds []map[string]any, err error) {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil, nil, fmt.Errorf("empty subscription")
	}

	// sing-box JSON form: a list of outbounds or {outbounds:[...]}
	if strings.HasPrefix(text, "[") || strings.HasPrefix(text, "{") {
		var arr []map[string]any
		if err := json.Unmarshal([]byte(text), &arr); err == nil {
			return nil, filterNodeOutbounds(arr), nil
		}
		var full struct {
			Outbounds []map[string]any `json:"outbounds"`
		}
		if err := json.Unmarshal([]byte(text), &full); err == nil {
			return nil, filterNodeOutbounds(full.Outbounds), nil
		}
	}

	// base64-wrapped v2ray subscription
	if dec, err := b64Decode(text); err == nil {
		decText := string(dec)
		if looksLikeURIs(decText) {
			text = decText
		}
	}

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		uris = append(uris, line)
	}
	if len(uris) == 0 {
		return nil, nil, fmt.Errorf("no nodes found in subscription")
	}
	return uris, nil, nil
}

func looksLikeURIs(s string) bool {
	return strings.Contains(s, "://")
}

// filterNodeOutbounds keeps real server outbounds, dropping the meta
// types (direct/block/selector/urltest/socks/http/dns).
func filterNodeOutbounds(ob []map[string]any) []map[string]any {
	var out []map[string]any
	for _, o := range ob {
		t, _ := o["type"].(string)
		switch t {
		case "direct", "block", "dns", "selector", "urltest", "socks", "http", "ssh":
			continue
		}
		out = append(out, o)
	}
	return out
}

// pickNode selects the node at index (0-based, negative = from the end),
// converting a URI to an outbound when needed.
func pickNode(uris []string, objs []map[string]any, index int) (map[string]any, string, error) {
	if len(uris) > 0 {
		if index < 0 {
			index += len(uris)
		}
		if index < 0 || index >= len(uris) {
			return nil, "", fmt.Errorf("subscription index %d out of range (%d nodes)", index, len(uris))
		}
		uri := uris[index]
		ob, err := parseNodeURI(uri)
		if err != nil {
			return nil, "", err
		}
		return ob, uri, nil
	}
	if len(objs) > 0 {
		if index < 0 {
			index += len(objs)
		}
		if index < 0 || index >= len(objs) {
			return nil, "", fmt.Errorf("subscription index %d out of range (%d nodes)", index, len(objs))
		}
		o := objs[index]
		if t, _ := o["tag"].(string); t == "" {
			o["tag"] = fmt.Sprintf("node%d", index)
		}
		return o, fmt.Sprintf("tag=%v", o["tag"]), nil
	}
	return nil, "", fmt.Errorf("subscription is empty")
}
