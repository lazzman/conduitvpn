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
	"sync"
	"sync/atomic"
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

	state       State
	stateDetail string
	current     *node.Node
	blacklist   map[string]state.BlacklistEntry
	mu          sync.Mutex

	// Route mode (auto / country / fixed)
	routeMode    string
	routeCountry string
	routeNode    string
	modeMu       sync.Mutex
	modeCh       chan struct{}

	startedAt  time.Time
	refreshCh  chan struct{}
	refreshReq atomic.Bool
}

// errModeChanged aborts the current connect/monitor cycle so the loop
// re-evaluates candidates under the new route mode without blacklisting
// the node that was just disconnected.
var errModeChanged = errors.New("route mode changed")

// Snapshot is a point-in-time view for the web UI.
type Snapshot struct {
	State          string     `json:"state"`
	Detail         string     `json:"detail"`
	CurrentNode    *node.Node `json:"current_node,omitempty"`
	BlacklistCount int        `json:"blacklist_count"`
	UptimeSec      int64      `json:"uptime_sec"`
	RouteMode      string     `json:"route_mode"`
	RouteCountry   string     `json:"route_country,omitempty"`
	RouteNode      string     `json:"route_node,omitempty"`
}

func New(cfg config.Config) *Manager {
	m := &Manager{
		cfg:          cfg,
		store:        state.NewStore(cfg.DataDir),
		prober:       health.NewProber(cfg.HealthAddr, cfg.ProbeTimeout),
		state:        StateIdle,
		blacklist:    map[string]state.BlacklistEntry{},
		startedAt:    time.Now(),
		refreshCh:    make(chan struct{}, 1),
		modeCh:       make(chan struct{}, 1),
		routeMode:    cfg.RouteMode,
		routeCountry: cfg.RouteCountry,
		routeNode:    cfg.RouteNode,
	}
	// Persisted route config (set via the web UI) wins over env defaults.
	if r, err := m.store.LoadRoute(); err == nil && r.Mode != "" {
		m.routeMode, m.routeCountry, m.routeNode = r.Mode, r.Country, r.Node
		logx.Info("route config loaded", "mode", r.Mode, "country", r.Country, "node", r.Node)
	}
	return m
}

// Snapshot returns the current supervisor view.
func (m *Manager) Snapshot() Snapshot {
	m.mu.Lock()
	s := Snapshot{
		State:          string(m.state),
		Detail:         m.stateDetail,
		CurrentNode:    m.current,
		BlacklistCount: len(m.blacklist),
		UptimeSec:      int64(time.Since(m.startedAt).Seconds()),
	}
	m.mu.Unlock()
	mode, country, node := m.routeConfig()
	s.RouteMode, s.RouteCountry, s.RouteNode = mode, country, node
	return s
}

// TriggerFetch asks the supervisor to refresh the node pool now.
func (m *Manager) TriggerFetch() {
	if !m.refreshReq.Swap(true) {
		select {
		case m.refreshCh <- struct{}{}:
		default:
		}
	}
}

// Run is the supervisor loop. It blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	m.loadBlacklist()
	for {
		if ctx.Err() != nil {
			return nil
		}
		m.refreshReq.Store(false) // consume any pending refresh request
		nodes := m.fetchAndBench(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if len(nodes) == 0 {
			logx.Warn("no candidate nodes available; retrying in 30s")
			if !m.wait(ctx, 30*time.Second) {
				return nil
			}
			continue
		}
		candidates := m.selectCandidates(nodes)
		if len(candidates) == 0 {
			mode, country, node := m.routeConfig()
			logx.Error("no candidates for current route mode", "mode", mode, "country", country, "node", node)
			if !m.wait(ctx, 20*time.Second) {
				return nil
			}
			continue
		}
		err := m.connectLoop(ctx, candidates)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errModeChanged) {
			logx.Info("route mode changed; re-evaluating candidates")
			continue
		}
		m.setState(StateDrifting, "all candidates exhausted")
		logx.Error("connect loop exhausted candidates", "err", err)
		if !m.wait(ctx, 15*time.Second) {
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
// The fixed-mode node is never blacklisted (it is the user's explicit
// lock choice).
func (m *Manager) connectLoop(ctx context.Context, nodes []*node.Node) error {
	for _, n := range nodes {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if m.isBlacklisted(n) && !m.isFixedNode(n) {
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
			if errors.Is(err, errModeChanged) {
				return errModeChanged
			}
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
		case <-m.modeCh:
			return errModeChanged
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
	m.mu.Lock()
	m.state = s
	m.stateDetail = detail
	m.mu.Unlock()
	logx.Info("state", "state", string(s), "detail", detail)
}

// --- route mode ---

// selectCandidates narrows the benchmarked pool per the active mode:
// auto keeps everything, country filters by CountryShort, fixed locks to
// one node by hostname or IP.
func (m *Manager) selectCandidates(nodes []*node.Node) []*node.Node {
	mode, country, fixed := m.routeConfig()
	switch mode {
	case "country":
		wanted := map[string]bool{}
		for _, c := range strings.Split(country, ",") {
			c = strings.ToUpper(strings.TrimSpace(c))
			if c != "" {
				wanted[c] = true
			}
		}
		var out []*node.Node
		for _, n := range nodes {
			if wanted[strings.ToUpper(n.CountryShort)] {
				out = append(out, n)
			}
		}
		return out
	case "fixed":
		for _, n := range nodes {
			if n.HostName == fixed || n.IP == fixed {
				return []*node.Node{n}
			}
		}
		return nil
	default:
		return nodes
	}
}

// isFixedNode reports whether n is the locked fixed-mode node.
func (m *Manager) isFixedNode(n *node.Node) bool {
	_, _, fixed := m.routeConfig()
	return fixed != "" && (n.HostName == fixed || n.IP == fixed)
}

func (m *Manager) routeConfig() (string, string, string) {
	m.modeMu.Lock()
	defer m.modeMu.Unlock()
	return m.routeMode, m.routeCountry, m.routeNode
}

// RouteConfig returns the active routing mode, country and node.
func (m *Manager) RouteConfig() (mode, country, node string) {
	return m.routeConfig()
}

// SetRouteConfig validates and applies a new routing mode, persists it
// and wakes the supervisor so the change takes effect immediately.
func (m *Manager) SetRouteConfig(mode, country, node string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	country = strings.ToUpper(strings.TrimSpace(country))
	node = strings.TrimSpace(node)
	switch mode {
	case "auto", "country", "fixed":
	default:
		return fmt.Errorf("invalid route mode %q (want auto|country|fixed)", mode)
	}
	if mode == "country" && country == "" {
		return errors.New("country mode requires a country code (e.g. JP)")
	}
	if mode == "fixed" && node == "" {
		return errors.New("fixed mode requires a node hostname or IP")
	}

	m.modeMu.Lock()
	m.routeMode, m.routeCountry, m.routeNode = mode, country, node
	m.modeMu.Unlock()

	if err := m.store.SaveRoute(state.Route{Mode: mode, Country: country, Node: node}); err != nil {
		logx.Warn("save route config failed", "err", err)
	}
	logx.Info("route mode set", "mode", mode, "country", country, "node", node)

	select {
	case m.modeCh <- struct{}{}:
	default:
	}
	return nil
}

// wait sleeps d unless the context dies or a refresh is requested.
// Returns false when the loop should restart early.
func (m *Manager) wait(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-m.refreshCh:
		return false
	case <-time.After(d):
		return true
	}
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
