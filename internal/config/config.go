// Package config loads typed configuration from environment variables.
// Env names mirror the legacy Python version where it makes sense, so
// existing deployments keep working with the same knobs.
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	DataDir          string
	APIURL           string
	FetchTimeout     time.Duration
	FetchInterval    time.Duration
	TargetValidNodes int
	MaxScanRows      int
	BenchConcurrency int
	BenchTimeout     time.Duration
	LogLevel         string

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
		DataDir:          envStr("AIMILI_DATA_DIR", "/data/aimilivpn"),
		APIURL:           envStr("VPNGATE_API_URL", "https://www.vpngate.net/api/iphone/"),
		FetchTimeout:     time.Duration(envInt("FETCH_TIMEOUT_SECONDS", 30)) * time.Second,
		FetchInterval:    time.Duration(envInt("FETCH_INTERVAL_SECONDS", 1260)) * time.Second,
		TargetValidNodes: envInt("TARGET_VALID_NODES", 3),
		MaxScanRows:      envInt("MAX_SCAN_ROWS", 300),
		BenchConcurrency: envInt("BENCH_CONCURRENCY", 50),
		BenchTimeout:     time.Duration(envInt("BENCH_TIMEOUT_SECONDS", 10)) * time.Second,
		LogLevel:         envStr("LOG_LEVEL", "info"),

		ConnectTimeout:    time.Duration(envInt("CONNECT_TIMEOUT_SECONDS", 40)) * time.Second,
		ProbeSettle:       time.Duration(envInt("PROBE_SETTLE_SECONDS", 2)) * time.Second,
		ProbeInterval:     time.Duration(envInt("PROBE_INTERVAL_SECONDS", 5)) * time.Second,
		ProbeTimeout:      time.Duration(envInt("PROBE_TIMEOUT_SECONDS", 5)) * time.Second,
		HealthMaxFails:    envInt("HEALTH_MAX_FAILS", 3),
		InitialProbeTries: envInt("INITIAL_PROBE_TRIES", 3),
		HealthAddr:        envStr("HEALTH_ADDR", "8.8.8.8:443"),
		OpenVPNAuthUser:   envStr("OPENVPN_AUTH_USER", "vpn"),
		OpenVPNAuthPass:   envStr("OPENVPN_AUTH_PASS", "vpn"),
	}
}
