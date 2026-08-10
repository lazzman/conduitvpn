// AimiliVPN — VPNGate 代理网关管理器（Go 重写版）。
// M2: 常驻守护进程。拉取 → 测速 → openvpn 连接（方案 B）→ HTTPS 探测 → 漂移。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"aimilivpn/internal/config"
	"aimilivpn/internal/logx"
	"aimilivpn/internal/manager"
	"aimilivpn/internal/tunnel"
)

func main() {
	cfg := config.Load()
	logx.Init(cfg.LogLevel)

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		logx.Error("cannot create data dir", "dir", cfg.DataDir, "err", err)
		os.Exit(1)
	}
	if v, err := tunnel.Version(); err != nil {
		logx.Error("openvpn unavailable", "err", err)
		os.Exit(1)
	} else {
		logx.Info("openvpn found", "version", v)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	m := manager.New(cfg)
	logx.Info("aimilivpn starting", "data_dir", cfg.DataDir)
	if err := m.Run(ctx); err != nil && ctx.Err() == nil {
		logx.Error("manager exited", "err", err)
		os.Exit(1)
	}
	logx.Info("aimilivpn stopped")
}
