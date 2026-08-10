// AimiliVPN — VPNGate proxy gateway manager.
// M1: fetch → parse → benchmark → persist. Runs as a one-shot CLI.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"aimilivpn/internal/benchmark"
	"aimilivpn/internal/config"
	"aimilivpn/internal/logx"
	"aimilivpn/internal/node"
	"aimilivpn/internal/state"
	"aimilivpn/internal/vpngate"
)

func main() {
	cfg := config.Load()
	logx.Init(cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logx.Error("cannot create data dir", "dir", cfg.DataDir, "err", err)
		os.Exit(1)
	}
	store := state.NewStore(cfg.DataDir)

	logx.Info("fetching vpngate nodes", "url", cfg.APIURL)
	raw, err := vpngate.Fetch(ctx, cfg.APIURL, cfg.FetchTimeout)
	if err != nil {
		logx.Error("fetch failed", "err", err)
		os.Exit(1)
	}
	nodes, err := vpngate.Parse(raw)
	if err != nil {
		logx.Error("parse failed", "err", err)
		os.Exit(1)
	}
	logx.Info("parsed nodes", "count", len(nodes))

	// Prefer high-score nodes, cap the scan window.
	candidates := node.SortByScore(nodes)
	if len(candidates) > cfg.MaxScanRows {
		candidates = candidates[:cfg.MaxScanRows]
	}

	logx.Info("benchmarking candidates", "count", len(candidates), "concurrency", cfg.BenchConcurrency)
	benchmark.Run(ctx, candidates, cfg.BenchConcurrency, cfg.BenchTimeout)

	if err := store.SaveNodes(candidates); err != nil {
		logx.Error("cannot save nodes", "err", err)
		os.Exit(1)
	}
	logx.Info("nodes saved", "path", store.NodesPath(), "count", len(candidates))

	top := node.TopByLatency(candidates, 5)
	fmt.Println("=== Top 5 by measured latency ===")
	if len(top) == 0 {
		fmt.Println("(no reachable node — check outbound connectivity)")
	}
	for _, n := range top {
		fmt.Printf("%-3s  %-24s  %6d ms  %-15s  %s\n",
			n.CountryShort, n.HostName, n.LatencyMS, n.RemoteAddr(), n.RemoteProto)
	}
}
