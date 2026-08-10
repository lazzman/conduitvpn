package upstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"conduitvpn/internal/config"
	"conduitvpn/internal/logx"
)

// Runtime holds the sing-box supervisor (nil when not in use).
type Runtime struct {
	Runner *Runner
}

// Start resolves the effective upstream proxy for node fetching.
//
// Priority: a sing-box source (URI → subscription → config file) overrides
// the legacy OPENVPN_UPSTREAM_*/BO_* envs. When a sing-box source is
// configured, sing-box runs as a local SOCKS5 gateway and the returned
// proxy points at it. When nothing sing-box related is configured, the
// legacy proxy (or nil = direct) is returned as-is.
//
// The returned cleanup function stops sing-box; it is nil when no
// sing-box process was started.
func Start(ctx context.Context, cfg *config.Config, dataDir string) (*config.UpstreamProxy, *Runtime, error) {
	// ---- no sing-box source: legacy behavior ----
	if cfg.UpstreamSingboxURI == "" && cfg.UpstreamSubscription == "" && cfg.UpstreamSingboxConfig == "" {
		return cfg.UpstreamProxy, nil, nil
	}

	port := cfg.UpstreamSingboxPort
	if port <= 0 {
		port = 10800
	}

	// ---- resolve the outbound ----
	var outbound map[string]any
	switch {
	case cfg.UpstreamSingboxURI != "":
		ob, err := parseNodeURI(cfg.UpstreamSingboxURI)
		if err != nil {
			return nil, nil, fmt.Errorf("sing-box uri: %w", err)
		}
		outbound = ob
		logx.Info("sing-box node from uri", "tag", ob["tag"], "server", ob["server"], "type", ob["type"])

	case cfg.UpstreamSubscription != "":
		raw, err := fetchSubscription(ctx, cfg.UpstreamSubscription, 30*time.Second)
		if err != nil {
			return nil, nil, fmt.Errorf("subscription fetch: %w", err)
		}
		uris, objs, err := parseSubscription(raw)
		if err != nil {
			return nil, nil, fmt.Errorf("subscription parse: %w", err)
		}
		ob, _, err := pickNode(uris, objs, cfg.UpstreamSingboxIndex)
		if err != nil {
			return nil, nil, fmt.Errorf("subscription pick: %w", err)
		}
		outbound = ob
		logx.Info("sing-box node from subscription", "tag", ob["tag"], "server", ob["server"], "type", ob["type"], "index", cfg.UpstreamSingboxIndex)

	case cfg.UpstreamSingboxConfig != "":
		raw := []byte(cfg.UpstreamSingboxConfig)
		if !json.Valid(raw) {
			b, err := os.ReadFile(cfg.UpstreamSingboxConfig)
			if err != nil {
				return nil, nil, fmt.Errorf("sing-box config: %w", err)
			}
			raw = b
		}
		var full map[string]any
		if err := json.Unmarshal(raw, &full); err != nil {
			return nil, nil, fmt.Errorf("sing-box config json: %w", err)
		}
		if p, err := socksInboundPort(full); err == nil {
			// full config with a socks inbound: run as-is
			port = p
			cfgPath, err := writeConfig(dataDir, full)
			if err != nil {
				return nil, nil, err
			}
			runner, err := StartRunner(ctx, cfgPath, port)
			if err != nil {
				return nil, nil, err
			}
			return &config.UpstreamProxy{Type: "socks5", Addr: net.JoinHostPort("127.0.0.1", fmt.Sprint(port))}, &Runtime{Runner: runner}, nil
		}
		// single outbound object
		outbound = full
		if t, _ := outbound["tag"].(string); t == "" {
			outbound["tag"] = "node"
		}
		logx.Info("sing-box outbound from config", "tag", outbound["tag"], "type", outbound["type"])
	}

	if outbound == nil {
		return nil, nil, fmt.Errorf("no sing-box outbound resolved")
	}

	cfgJSON := buildSingBoxConfig(outbound, port)
	cfgPath, err := writeConfig(dataDir, cfgJSON)
	if err != nil {
		return nil, nil, err
	}
	runner, err := StartRunner(ctx, cfgPath, port)
	if err != nil {
		return nil, nil, err
	}
	return &config.UpstreamProxy{Type: "socks5", Addr: net.JoinHostPort("127.0.0.1", fmt.Sprint(port))}, &Runtime{Runner: runner}, nil
}

// Stop terminates the sing-box process, if any.
func (rt *Runtime) Stop() {
	if rt != nil && rt.Runner != nil {
		rt.Runner.Stop()
	}
}
