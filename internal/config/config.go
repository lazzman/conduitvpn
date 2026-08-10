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
	}
}
