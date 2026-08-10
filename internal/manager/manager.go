// Package manager is the supervisor: it owns the tunnel lifecycle
// (fetch → connect → probe → drift) as a single blocking loop. 方案 B
// means openvpn's pushed redirect-gateway handles routing, so the
// manager has no policy-routing or SO_BINDTODEVICE concerns.
package manager

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aimilivpn/internal/benchmark"
	"aimilivpn/internal/config"
	"aimilivpn/internal/health"
	"aimilivpn/internal/logx"
	"aimilivpn/internal/node"
	"aimilivpn/internal/state"
	"aimilivpn/internal/tunnel"
	"aimilivpn/internal/vpngate"
)

type State string

const (
	StateIdle       State = "idle"
	StateFetching   State = "fetching"
	StateConnecting State = "connecting"
	StateConnected  State = "connected"
	StateDrifting   State = "drifting"
)

type Manager struct {
	cfg    config.Config
	store  *state.Store
	prober *health.Prober
	tun    *tunnel.Tunnel

	state     State
	current   *node.Node
	blacklist map[string]state.BlacklistEntry
}

func New(cfg config.Config) *Manager {
	return &Manager{
		cfg:       cfg,
		store:     state.NewStore(cfg.DataDir),
		prober:    health.NewProber(cfg.HealthAddr, cfg.ProbeTimeout),
		state:     StateIdle,
		blacklist: map[string]state.BlacklistEntry{},
	}
}

// Run is the supervisor loop. It blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	m.loadBlacklist()
	for {
		if ctx.Err() != nil {
			return nil
		}
		nodes := m.fetchAndBench(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if len(nodes) == 0 {
			logx.Warn("no candidate nodes available; retrying in 30s")
			if !sleepCtx(ctx, 30*time.Second) {
				return nil
			}
			continue
		}
		err := m.connectLoop(ctx, nodes)
		if ctx.Err() != nil {
			return nil
		}
		m.setState(StateDrifting, "all candidates exhausted")
		logx.Error("connect loop exhausted candidates", "err", err)
		if !sleepCtx(ctx, 15*time.Second) {
			return nil
		}
	}
}

// fetchAndBench refreshes the candidate pool. On network failure it falls
// back to the last persisted node list so a transient outage does not
// kill the daemon.
func (m *Manager) fetchAndBench(ctx context.Context) []*node.Node {
	m.setState(StateFetching, "")
	raw, err := vpngate.Fetch(ctx, m.cfg.APIURL, m.cfg.FetchTimeout)
	if err != nil {
		logx.Warn("fetch failed; falling back to cached nodes", "err", err)
		cached, cerr := m.store.LoadNodes()
		if cerr != nil || len(cached) == 0 {
			return nil
		}
		return cached
	}
	nodes, err := vpngate.Parse(raw)
	if err != nil {
		logx.Error("parse failed", "err", err)
		return nil
	}
	candidates := node.SortByScore(nodes)
	if len(candidates) > m.cfg.MaxScanRows {
		candidates = candidates[:m.cfg.MaxScanRows]
	}
	logx.Info("benchmarking candidates", "count", len(candidates))
	benchmark.Run(ctx, candidates, m.cfg.BenchConcurrency, m.cfg.BenchTimeout)
	if err := m.store.SaveNodes(candidates); err != nil {
		logx.Warn("save nodes failed", "err", err)
	}
	return candidates
}

// connectLoop tries nodes best-first until one connects and stays
// healthy; each failure blacklists the node and moves to the next.
func (m *Manager) connectLoop(ctx context.Context, nodes []*node.Node) error {
	for _, n := range nodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if m.isBlacklisted(n) {
			continue
		}
		if err := m.connectAndVerify(ctx, n); err != nil {
			logx.Warn("node failed", "host", n.HostName, "err", err)
			m.markBlacklisted(n, err.Error())
			continue
		}
		err := m.monitor(ctx, n)
		if m.tun != nil {
			_ = m.tun.Stop()
			m.tun = nil
		}
		if err != nil {
			logx.Warn("node degraded, drifting", "host", n.HostName, "err", err)
			m.markBlacklisted(n, err.Error())
			continue
		}
		return ctx.Err()
	}
	return errors.New("all candidates exhausted")
}

// connectAndVerify spawns openvpn, waits for the handshake, lets the
// tunnel settle, then confirms egress with the HTTPS probe.
func (m *Manager) connectAndVerify(ctx context.Context, n *node.Node) error {
	m.setState(StateConnecting, n.HostName)
	m.current = n

	cfgPath, authPath, err := m.prepareFiles(n)
	if err != nil {
		return fmt.Errorf("prepare files: %w", err)
	}

	tun := tunnel.New()
	m.tun = tun
	if err := tun.Start(tunnel.Options{ConfigFile: cfgPath, AuthFile: authPath}); err != nil {
		return fmt.Errorf("spawn openvpn: %w", err)
	}
	if err := tun.WaitHandshake(m.cfg.ConnectTimeout); err != nil {
		_ = tun.Stop()
		return err
	}

	if !sleepCtx(ctx, m.cfg.ProbeSettle) {
		_ = tun.Stop()
		return ctx.Err()
	}

	var lastErr error
	for i := 0; i < m.cfg.InitialProbeTries; i++ {
		probeErr := m.prober.Check(ctx)
		if probeErr == nil {
			m.setState(StateConnected, n.HostName)
			logx.Info("tunnel healthy", "host", n.HostName, "remote", n.RemoteAddr())
			return nil
		}
		lastErr = probeErr
		logx.Warn("initial probe failed", "host", n.HostName, "try", i+1, "err", probeErr)
		if !sleepCtx(ctx, m.cfg.ProbeInterval) {
			_ = tun.Stop()
			return ctx.Err()
		}
	}
	_ = tun.Stop()
	return fmt.Errorf("initial probe degraded: %v", lastErr)
}

// monitor runs the liveness loop while connected: HTTPS probe every
// interval; N consecutive failures or an unexpected openvpn exit drift.
func (m *Manager) monitor(ctx context.Context, n *node.Node) error {
	m.setState(StateConnected, n.HostName)
	fails := 0
	ticker := time.NewTicker(m.cfg.ProbeInterval)
	defer ticker.Stop()

	tunEvents := m.tun.Events()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-tunEvents:
			if !ok {
				return errors.New("tunnel event channel closed")
			}
			if ev.Type == tunnel.EventExit {
				return fmt.Errorf("openvpn exited: %s", ev.Line)
			}
		case <-ticker.C:
			if err := m.prober.Check(ctx); err != nil {
				fails++
				logx.Warn("probe failed", "host", n.HostName, "fails", fails, "err", err)
				if fails >= m.cfg.HealthMaxFails {
					return errors.New("health degraded: consecutive probe failures")
				}
			} else {
				if fails > 0 {
					logx.Info("probe recovered", "host", n.HostName, "after_fails", fails)
				}
				fails = 0
			}
		}
	}
}

// --- helpers ---

func (m *Manager) prepareFiles(n *node.Node) (string, string, error) {
	cfgDir := filepath.Join(m.cfg.DataDir, "configs")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		return "", "", err
	}
	name := sanitizeName(n.HostName)
	if name == "" {
		name = sanitizeName(n.IP)
	}
	cfgPath := filepath.Join(cfgDir, name+".ovpn")
	if err := os.WriteFile(cfgPath, []byte(n.ConfigText), 0o600); err != nil {
		return "", "", err
	}
	authPath := filepath.Join(m.cfg.DataDir, "openvpn.auth")
	auth := fmt.Sprintf("%s\n%s\n", m.cfg.OpenVPNAuthUser, m.cfg.OpenVPNAuthPass)
	if err := os.WriteFile(authPath, []byte(auth), 0o600); err != nil {
		return "", "", err
	}
	return cfgPath, authPath, nil
}

func (m *Manager) loadBlacklist() {
	bl, err := m.store.LoadBlacklist()
	if err != nil {
		logx.Debug("no blacklist yet", "err", err)
		return
	}
	m.blacklist = bl
	logx.Info("blacklist loaded", "count", len(bl))
}

func (m *Manager) isBlacklisted(n *node.Node) bool {
	_, ok := m.blacklist[n.HostName]
	return ok
}

func (m *Manager) markBlacklisted(n *node.Node, reason string) {
	m.blacklist[n.HostName] = state.BlacklistEntry{
		Reason:   reason,
		MarkedAt: time.Now().Format(time.RFC3339),
	}
	if err := m.store.SaveBlacklist(m.blacklist); err != nil {
		logx.Warn("save blacklist failed", "err", err)
	}
	logx.Warn("node blacklisted", "host", n.HostName, "reason", reason)
}

func (m *Manager) setState(s State, detail string) {
	m.state = s
	logx.Info("state", "state", string(s), "detail", detail)
}

func sanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
