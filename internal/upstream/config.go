package upstream

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// buildSingBoxConfig assembles a minimal runnable sing-box config: one
// local SOCKS5 inbound plus the node outbound.
func buildSingBoxConfig(outbound map[string]any, port int) map[string]any {
	return map[string]any{
		"log": map[string]any{"level": "warn"},
		"inbounds": []any{
			map[string]any{"type": "socks", "listen": "127.0.0.1", "listen_port": port},
		},
		"outbounds": []any{
			outbound,
			map[string]any{"type": "direct", "tag": "direct"},
		},
	}
}

// writeConfig persists the sing-box config JSON and returns its path.
func writeConfig(dataDir string, cfg map[string]any) (string, error) {
	dir := filepath.Join(dataDir, "singbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// socksInboundPort extracts the port of the first socks inbound from a
// user-provided full sing-box config.
func socksInboundPort(cfg map[string]any) (int, error) {
	inbounds, _ := cfg["inbounds"].([]any)
	for _, raw := range inbounds {
		ib, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := ib["type"].(string); t == "socks" {
			switch p := ib["listen_port"].(type) {
			case float64:
				return int(p), nil
			case string:
				return strconv.Atoi(p)
			}
		}
	}
	return 0, fmt.Errorf("no socks inbound found in sing-box config")
}
