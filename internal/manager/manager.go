// Package manager is the supervisor: it owns the tunnel lifecycle
// (fetch → connect → probe → drift) as a single blocking loop and enables
// host-mode egress only after OpenVPN has established its device.
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

	"conduitvpn/internal/benchmark"
	"conduitvpn/internal/config"
	"conduitvpn/internal/egress"
	"conduitvpn/internal/health"
	"conduitvpn/internal/logx"
	"conduitvpn/internal/node"
	"conduitvpn/internal/state"
	"conduitvpn/internal/tunnel"
	"conduitvpn/internal/upstream"
	"conduitvpn/internal/vpngate"
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
	egress *egress.Controller
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

	// Effective upstream for node fetching (sing-box or legacy)
	fetchUpstream *config.UpstreamProxy
	upstreamRT    *upstream.Runtime

	startedAt   time.Time
	refreshCh   chan struct{}
	refreshReq  atomic.Bool
	demo        bool
	demoRefresh uint32
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
	egressCtl := egress.New(cfg.NetworkMode)
	m := &Manager{
		cfg:          cfg,
		store:        state.NewStore(cfg.DataDir),
		prober:       health.NewProber(cfg.HealthAddr, cfg.ProbeTimeout, egressCtl),
		egress:       egressCtl,
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

// Egress exposes the supervisor-owned application egress policy to the
// proxy server. Its readiness tracks the tunnel lifecycle.
func (m *Manager) Egress() *egress.Controller { return m.egress }

// NewDemo returns a manager backed by deterministic sample data. It never
// starts an upstream, tunnel, benchmark, or health probe.
func NewDemo(cfg config.Config) *Manager {
	m := New(cfg)
	m.demo = true
	m.refreshDemoNodes()
	logx.Info("demo manager ready", "nodes", len(demoNodes(0)))
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
	if m.demo {
		m.refreshDemoNodes()
		return
	}
	if !m.refreshReq.Swap(true) {
		select {
		case m.refreshCh <- struct{}{}:
		default:
		}
	}
}

func (m *Manager) refreshDemoNodes() {
	refresh := atomic.AddUint32(&m.demoRefresh, 1)
	nodes := demoNodes(refresh)
	if err := m.store.SaveNodes(nodes); err != nil {
		logx.Warn("save demo nodes failed", "err", err)
	}
	m.updateDemoCurrent(nodes)
	logx.Info("demo nodes refreshed", "count", len(nodes))
}

func (m *Manager) updateDemoCurrent(nodes []*node.Node) {
	candidates := m.selectCandidates(nodes)
	if len(candidates) == 0 {
		m.setState(StateDrifting, "demo route has no matching node")
		return
	}
	m.mu.Lock()
	m.current = candidates[0]
	m.state = StateConnected
	m.stateDetail = candidates[0].HostName
	m.mu.Unlock()
	logx.Info("state", "state", string(StateConnected), "detail", candidates[0].HostName)
}

func demoNodes(refresh uint32) []*node.Node {
	latencyOffset := int(refresh%3) * 7
	return []*node.Node{
		{HostName: "demo-tokyo-01", IP: "203.0.113.10", Score: 9821, Ping: 18, Speed: 180000000, CountryLong: "Japan", CountryShort: "JP", Sessions: 42, Uptime: 864000, Operator: "Demo Tokyo", RemoteHost: "203.0.113.10", RemotePort: 1194, RemoteProto: "udp", Tested: true, LatencyMS: 48 + latencyOffset},
		{HostName: "demo-seoul-01", IP: "203.0.113.20", Score: 9140, Ping: 25, Speed: 150000000, CountryLong: "South Korea", CountryShort: "KR", Sessions: 31, Uptime: 640000, Operator: "Demo Seoul", RemoteHost: "203.0.113.20", RemotePort: 1194, RemoteProto: "udp", Tested: true, LatencyMS: 72 + latencyOffset},
		{HostName: "demo-singapore-01", IP: "203.0.113.30", Score: 8860, Ping: 39, Speed: 120000000, CountryLong: "Singapore", CountryShort: "SG", Sessions: 24, Uptime: 432000, Operator: "Demo Singapore", RemoteHost: "203.0.113.30", RemotePort: 443, RemoteProto: "tcp", Tested: true, LatencyMS: 96 + latencyOffset},
		{HostName: "demo-frankfurt-01", IP: "203.0.113.40", Score: 8010, Ping: 53, Speed: 95000000, CountryLong: "Germany", CountryShort: "DE", Sessions: 18, Uptime: 259200, Operator: "Demo Frankfurt", RemoteHost: "203.0.113.40", RemotePort: 1194, RemoteProto: "udp", Tested: true, LatencyMS: 164 + latencyOffset},
		{HostName: "demo-newyork-01", IP: "203.0.113.50", Score: 7450, Ping: 78, Speed: 76000000, CountryLong: "United States", CountryShort: "US", Sessions: 11, Uptime: 172800, Operator: "Demo New York", RemoteHost: "203.0.113.50", RemotePort: 443, RemoteProto: "tcp", Tested: true, LatencyMS: 218 + latencyOffset},
	}
}

// Run is the supervisor loop. It blocks until ctx is cancelled.
func (m *Manager) Run(ctx context.Context) error {
	// Resolve the effective upstream: sing-box sources take precedence
	// over the legacy OPENVPN_UPSTREAM_*/BO_* envs.
	proxy, rt, err := upstream.Start(ctx, &m.cfg, m.cfg.DataDir)
	if err != nil {
		return fmt.Errorf("upstream init: %w", err)
	}
	m.fetchUpstream = proxy
	m.upstreamRT = rt
	if rt != nil {
		defer rt.Stop()
	}
	if m.fetchUpstream != nil {
		logx.Info("using upstream proxy for node fetch", "type", m.fetchUpstream.Type, "addr", m.fetchUpstream.Addr)
	}
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
	client := vpngate.NewClient(m.fetchUpstream, m.cfg.FetchTimeout)
	raw, err := client.Fetch(ctx, m.cfg.APIURL)
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
		m.stopTunnel()
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
	if err := tun.Start(tunnel.Options{
		ConfigFile:  cfgPath,
		AuthFile:    authPath,
		RouteNoPull: m.cfg.NetworkMode == "host",
	}); err != nil {
		return fmt.Errorf("spawn openvpn: %w", err)
	}
	if err := tun.WaitHandshake(m.cfg.ConnectTimeout); err != nil {
		m.stopTunnel()
		return err
	}
	if m.cfg.NetworkMode == "host" {
		if err := m.egress.Configure(tun.Device()); err != nil {
			m.stopTunnel()
			return fmt.Errorf("configure host tunnel egress: %w", err)
		}
	}

	if !sleepCtx(ctx, m.cfg.ProbeSettle) {
		m.stopTunnel()
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
	m.stopTunnel()
	return fmt.Errorf("initial probe degraded: %v", lastErr)
}

func (m *Manager) stopTunnel() {
	m.egress.Clear()
	if m.tun != nil {
		_ = m.tun.Stop()
		m.tun = nil
	}
}

// monitor runs the liveness loop while connected: HTTPS probe every
// interval; N consecutive failures or an unexpected openvpn exit drift.
// A separate ticker measures the current node's live TCP latency for
// the dashboard chart.
func (m *Manager) monitor(ctx context.Context, n *node.Node) error {
	m.setState(StateConnected, n.HostName)
	fails := 0
	ticker := time.NewTicker(m.cfg.ProbeInterval)
	defer ticker.Stop()
	latTicker := time.NewTicker(m.cfg.LatencyInterval)
	defer latTicker.Stop()

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
		case <-latTicker.C:
			m.measureLiveLatency(ctx, n)
		}
	}
}

// measureLiveLatency measures TCP connect time to the node's remote
// endpoint through the tunnel and updates the dashboard value.
func (m *Manager) measureLiveLatency(ctx context.Context, n *node.Node) {
	start := time.Now()
	conn, err := m.egress.DialContext(ctx, "tcp", n.RemoteAddr(), 3*time.Second, 0)
	if err != nil {
		return // node's port may be UDP-only; keep the last value
	}
	_ = conn.Close()
	ms := int(time.Since(start).Milliseconds())
	if ms < 1 {
		ms = 1
	}
	m.mu.Lock()
	if m.current != nil && m.current.HostName == n.HostName {
		m.current.LatencyMS = ms
	}
	m.mu.Unlock()
	logx.Debug("live latency", "host", n.HostName, "ms", ms)
}

// --- helpers ---

func (m *Manager) prepareFiles(n *node.Node) (string, string, error) {
	safeConfig, err := vpngate.ValidateOpenVPNProfile(n.ConfigText, n.IP)
	if err != nil {
		return "", "", fmt.Errorf("unsafe OpenVPN profile: %w", err)
	}
	cfgDir := filepath.Join(m.cfg.DataDir, "configs")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return "", "", err
	}
	if err := os.Chmod(cfgDir, 0o700); err != nil {
		return "", "", err
	}
	name := sanitizeName(n.HostName)
	if name == "" {
		name = sanitizeName(n.IP)
	}
	cfgPath := filepath.Join(cfgDir, name+".ovpn")
	if err := os.WriteFile(cfgPath, []byte(safeConfig), 0o600); err != nil {
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
	if m.demo {
		nodes, err := m.store.LoadNodes()
		if err != nil {
			logx.Warn("load demo nodes failed", "err", err)
		} else {
			m.updateDemoCurrent(nodes)
		}
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
