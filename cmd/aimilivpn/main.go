// AimiliVPN — VPNGate 代理网关管理器（Go 重写版）。
// 守护进程：proxy（单端口双协议）+ manager（隧道监督）+ webui（管理后台）。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"aimilivpn/internal/config"
	"aimilivpn/internal/logx"
	"aimilivpn/internal/manager"
	"aimilivpn/internal/proxy"
	"aimilivpn/internal/state"
	"aimilivpn/internal/tunnel"
	"aimilivpn/internal/webui"
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

	store := state.NewStore(cfg.DataDir)
	m := manager.New(cfg)

	proxySrv := proxy.New(cfg.LocalProxyHost, cfg.LocalProxyPort, cfg.ProxyUser, cfg.ProxyPass, cfg.DNSServer, cfg.ProxyMaxConns)
	if err := proxySrv.Start(); err != nil {
		logx.Error("proxy start failed", "err", err)
		os.Exit(1)
	}
	defer proxySrv.Close()
	logx.Info("proxy listening", "addr", proxySrv.Addr().String(), "dual", "http+socks5")

	ui := webui.New(cfg, store, m)
	if err := ui.Start(); err != nil {
		logx.Error("webui start failed", "err", err)
		os.Exit(1)
	}
	defer ui.Close()
	logx.Info("webui listening", "addr", ui.Addr().String(), "path", "/"+ui.SecretPath())

	logx.Info("aimilivpn starting", "data_dir", cfg.DataDir)
	if err := m.Run(ctx); err != nil && ctx.Err() == nil {
		logx.Error("manager exited", "err", err)
		os.Exit(1)
	}
	logx.Info("aimilivpn stopped")
}
