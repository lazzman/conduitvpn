// ConduitVPN — VPNGate 代理网关管理器（Go 重写版）。
// 守护进程：proxy（单端口双协议）+ manager（隧道监督）+ webui（管理后台）。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"conduitvpn/internal/config"
	"conduitvpn/internal/hy2"
	"conduitvpn/internal/logx"
	"conduitvpn/internal/manager"
	"conduitvpn/internal/netfix"
	"conduitvpn/internal/proxy"
	"conduitvpn/internal/state"
	"conduitvpn/internal/tunnel"
	"conduitvpn/internal/webui"
)

func main() {
	demo := flag.Bool("demo", false, "start the interactive Web UI demo without VPN or proxy services")
	flag.Parse()

	cfg := config.Load()
	if *demo {
		cfg.Demo = true
		cfg.DataDir = config.DemoDataDir()
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "invalid configuration:", err)
		os.Exit(2)
	}
	logx.Init(cfg.LogLevel)

	if err := state.SecureDir(cfg.DataDir); err != nil {
		logx.Error("cannot secure data dir", "dir", cfg.DataDir, "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := state.NewStore(cfg.DataDir)

	// Demo credentials are explicit, while production requires configured
	// credentials on first boot and migrates existing legacy files in place.
	var err error
	demoUser, demoPass := "", ""
	if *demo {
		demoUser, demoPass = demoCredential("UI_USER", "admin"), demoCredential("UI_PASSWORD", "demo")
		err = store.EnsureAuthConfigured(demoUser, demoPass, true)
	} else {
		err = store.EnsureAuthConfigured(cfg.UIUser, cfg.UIPassword, false)
	}
	if err != nil {
		logx.Error("webui auth init failed", "err", err)
		os.Exit(1)
	}
	var m *manager.Manager
	if *demo {
		m = manager.NewDemo(cfg)
	} else {
		if v, err := tunnel.Version(); err != nil {
			logx.Error("openvpn unavailable", "err", err)
			os.Exit(1)
		} else {
			logx.Info("openvpn found", "version", v)
		}

		// 方案 B 回包路由修复：入站 UI/代理连接的响应走 docker 网关，
		// 出站代理流量仍走 VPN。失败不影响主流程（仅入站回包降级）。
		udpPorts := []string{}
		if cfg.HY2Port > 0 {
			udpPorts = append(udpPorts, fmt.Sprint(cfg.HY2Port))
		}
		if err := netfix.Apply([]string{fmt.Sprint(cfg.UIPort), fmt.Sprint(cfg.LocalProxyPort)}, udpPorts); err != nil {
			logx.Warn("netfix skipped", "err", err)
		} else {
			logx.Info("netfix applied", "tcp", fmt.Sprint(cfg.UIPort)+","+fmt.Sprint(cfg.LocalProxyPort), "udp", strings.Join(udpPorts, ","))
		}

		// hy2 inbound gateway (hysteria2 clients → 方案 B tunnel)
		if cfg.HY2Port > 0 {
			hy2Runner, err := hy2.Start(ctx, cfg.DataDir, hy2.Config{
				Port:     cfg.HY2Port,
				Bind:     cfg.HY2Bind,
				Password: cfg.HY2Password,
				ObfsPass: cfg.HY2ObfsPassword,
			})
			if err != nil {
				logx.Error("hy2 start failed", "err", err)
				os.Exit(1)
			}
			defer hy2Runner.Stop()
		}

		proxySrv := proxy.New(cfg.LocalProxyHost, cfg.LocalProxyPort, cfg.ProxyUser, cfg.ProxyPass, cfg.DNSServer, cfg.ProxyMaxConns)
		if err := proxySrv.Start(); err != nil {
			logx.Error("proxy start failed", "err", err)
			os.Exit(1)
		}
		defer proxySrv.Close()
		logx.Info("proxy listening", "addr", proxySrv.Addr().String(), "dual", "http+socks5")
		m = manager.New(cfg)
	}

	ui := webui.New(cfg, store, m)
	if err := ui.Start(); err != nil {
		logx.Error("webui start failed", "err", err)
		os.Exit(1)
	}
	defer ui.Close()
	scheme := "http"
	if cfg.UITLSCert != "" {
		scheme = "https"
	}
	logx.Info("webui listening", "addr", ui.Addr().String(), "scheme", scheme, "path", "/"+ui.SecretPath()+"/", "auth", "login required")

	if *demo {
		logx.Info("conduitvpn demo starting", "data_dir", cfg.DataDir, "username", demoUser, "password", demoPass)
		<-ctx.Done()
		logx.Info("conduitvpn demo stopped")
		return
	}

	logx.Info("conduitvpn starting", "data_dir", cfg.DataDir)
	if err := m.Run(ctx); err != nil && ctx.Err() == nil {
		logx.Error("manager exited", "err", err)
		os.Exit(1)
	}
	logx.Info("conduitvpn stopped")
}

func demoCredential(env, fallback string) string {
	if value := os.Getenv(env); value != "" {
		return value
	}
	return fallback
}
