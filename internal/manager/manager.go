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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"conduitvpn/internal/config"
	"conduitvpn/internal/egress"
	"conduitvpn/internal/health"
	"conduitvpn/internal/logx"
	"conduitvpn/internal/node"
	"conduitvpn/internal/purity"
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

	state             State
	stateDetail       string
	current           *node.Node
	blacklist         map[string]state.BlacklistEntry
	blacklistTest     BlacklistTestStatus
	blacklistVerifier func(context.Context, *node.Node) error
	taskCtx           context.Context
	mu                sync.Mutex

	// Route mode (auto / country / fixed)
	routeMode    string
	routeCountry string
	routeNode    string
	modeMu       sync.Mutex
	modeCh       chan struct{}

	// Effective upstream for node fetching (sing-box or legacy)
	fetchUpstream *config.UpstreamProxy
	upstreamRT    *upstream.Runtime

	// VPNGate source configuration and ephemeral refresh status.
	sourceMu     sync.Mutex
	mirrors      []string
	sourceStatus VPNGateSourceStatus
	// mirrorValidator is injectable for deterministic manager tests; the
	// production default is vpngate.ValidateMirrorOrigin.
	mirrorValidator func(context.Context, string) error
	refreshMu       sync.Mutex

	// The latest benchmarked pool can be replaced while the current tunnel is
	// being monitored. Readers always receive a snapshot of the slice.
	candidateMu   sync.RWMutex
	candidatePool []*node.Node
	candidateCh   chan struct{}

	startedAt        time.Time
	refreshCh        chan struct{}
	refreshPendingMu sync.Mutex
	refreshSeq       uint64
	refreshConsumed  uint64
	demo             bool
	demoRefresh      uint32

	purityLookup  func(context.Context, string) (purity.Record, error)
	purityMu      sync.Mutex
	purityPending atomic.Int32
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
	PurityPending  int        `json:"purity_pending"`
}

const (
	BlacklistTestPending = "pending"
	BlacklistTestRunning = "running"
	BlacklistTestPassed  = "passed"
	BlacklistTestFailed  = "failed"
	BlacklistTestSkipped = "skipped"
)

var ErrBlacklistTestRunning = errors.New("blacklist verification is already running")

// BlacklistTestResult is the ephemeral result of one isolated VPN check.
// Results are intentionally not persisted: a restart requires a fresh check.
type BlacklistTestResult struct {
	Host     string `json:"host"`
	Status   string `json:"status"`
	Code     string `json:"code,omitempty"`
	Error    string `json:"error,omitempty"`
	TestedAt string `json:"tested_at,omitempty"`
}

// BlacklistTestStatus represents the current or most recent batch job.
type BlacklistTestStatus struct {
	Running    bool                  `json:"running"`
	StartedAt  string                `json:"started_at,omitempty"`
	FinishedAt string                `json:"finished_at,omitempty"`
	Total      int                   `json:"total"`
	Completed  int                   `json:"completed"`
	Results    []BlacklistTestResult `json:"results"`
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
		sourceStatus: VPNGateSourceStatus{Mirrors: []string{}, Attempts: []VPNGateSourceAttempt{}},
		taskCtx:      context.Background(),
		startedAt:    time.Now(),
		refreshCh:    make(chan struct{}, 1),
		candidateCh:  make(chan struct{}, 1),
		modeCh:       make(chan struct{}, 1),
		routeMode:    cfg.RouteMode,
		routeCountry: cfg.RouteCountry,
		routeNode:    cfg.RouteNode,
		purityLookup: purity.NewClient(12 * time.Second).Lookup,
	}
	m.mirrorValidator = vpngate.ValidateMirrorOrigin
	m.loadVPNGateSources()
	m.blacklistVerifier = m.verifyBlacklistedNode
	// Persisted route config (set via the web UI) wins over env defaults.
	if r, err := m.store.LoadRoute(); err == nil && r.Mode != "" {
		m.routeMode, m.routeCountry, m.routeNode = r.Mode, r.Country, r.Node
		logx.Info("route config loaded", "mode", r.Mode, "country", r.Country, "node", r.Node)
	}
	m.loadBlacklist()
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
		CurrentNode:    cloneNode(m.current),
		BlacklistCount: len(m.blacklist),
		UptimeSec:      int64(time.Since(m.startedAt).Seconds()),
	}
	m.mu.Unlock()
	mode, country, node := m.routeConfig()
	s.RouteMode, s.RouteCountry, s.RouteNode = mode, country, node
	s.PurityPending = int(m.purityPending.Load())
	return s
}

// TriggerFetch asks the supervisor to refresh the node pool now.
func (m *Manager) TriggerFetch() {
	if m.demo {
		m.refreshDemoNodes()
		return
	}
	m.requestRefresh()
}

func (m *Manager) refreshDemoNodes() {
	refresh := atomic.AddUint32(&m.demoRefresh, 1)
	nodes := demoNodes(refresh)
	if err := m.store.SaveNodes(nodes); err != nil {
		logx.Warn("save demo nodes failed", "err", err)
	}
	m.setCandidatePool(nodes)
	m.seedDemoPurity(nodes)
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
	ctx = nonNilContext(ctx)
	m.mu.Lock()
	m.taskCtx = ctx
	m.mu.Unlock()
	if m.demo {
		<-ctx.Done()
		return nil
	}
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
	// The first fetch remains synchronous so startup has a candidate pool
	// before the tunnel selection loop begins. Later refreshes are independent
	// of the connected tunnel and run in the background.
	nodes := m.fetchAndBench(ctx)
	if ctx.Err() != nil {
		return nil
	}
	m.drainCandidateSignals()
	go m.refreshLoop(ctx)
	for {
		if ctx.Err() != nil {
			return nil
		}
		if len(nodes) == 0 {
			nodes = m.candidateSnapshot()
		}
		if len(nodes) == 0 {
			logx.Warn("no candidate nodes available; waiting for the next refresh")
			m.requestRefresh()
			if !m.wait(ctx, m.refreshInterval()) && ctx.Err() != nil {
				return nil
			}
			nodes = m.candidateSnapshot()
			continue
		}
		candidates := m.selectCandidates(nodes)
		if len(candidates) == 0 {
			mode, country, node := m.routeConfig()
			logx.Error("no candidates for current route mode", "mode", mode, "country", country, "node", node)
			if !m.wait(ctx, m.refreshInterval()) && ctx.Err() != nil {
				return nil
			}
			nodes = m.candidateSnapshot()
			continue
		}
		err := m.connectLoop(ctx, candidates)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errModeChanged) {
			logx.Info("route mode changed; re-evaluating candidates")
			nodes = m.candidateSnapshot()
			continue
		}
		m.setState(StateDrifting, "all candidates exhausted")
		logx.Error("connect loop exhausted candidates", "err", err)
		m.requestRefresh()
		if !m.wait(ctx, m.refreshInterval()) && ctx.Err() != nil {
			return nil
		}
		nodes = m.candidateSnapshot()
	}
}

func (m *Manager) refreshInterval() time.Duration {
	if m.cfg.FetchInterval > 0 {
		return m.cfg.FetchInterval
	}
	return 1260 * time.Second
}

func (m *Manager) drainCandidateSignals() {
	for {
		select {
		case <-m.candidateCh:
		default:
			return
		}
	}
}

// fetchAndBench refreshes the candidate pool. On network failure it falls
// back to the last persisted node list so a transient outage does not
// kill the daemon.
func (m *Manager) fetchAndBench(ctx context.Context) []*node.Node {
	return m.refreshNodes(ctx, true)
}

// connectLoop tries nodes best-first until one connects and stays
// healthy; each failure blacklists the node and moves to the next.
// The fixed-mode node is never blacklisted (it is the user's explicit
// lock choice).
func (m *Manager) connectLoop(ctx context.Context, nodes []*node.Node) error {
	attempted := make(map[string]bool)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var n *node.Node
		for _, candidate := range nodes {
			if candidate != nil && !attempted[candidateKey(candidate)] {
				n = candidate
				break
			}
		}
		if n == nil {
			return errors.New("all candidates exhausted")
		}
		attempted[candidateKey(n)] = true
		if m.isBlacklisted(n) && !m.isFixedNode(n) {
			continue
		}
		if err := m.connectAndVerify(ctx, n); err != nil {
			logx.Warn("node failed", "host", n.HostName, "err", err)
			m.markBlacklisted(n, err.Error())
			if latest := m.candidateSnapshot(); len(latest) > 0 {
				nodes = m.selectCandidates(latest)
			}
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
			if latest := m.candidateSnapshot(); len(latest) > 0 {
				nodes = m.selectCandidates(latest)
			}
			continue
		}
		return ctx.Err()
	}
}

func candidateKey(n *node.Node) string {
	if n == nil {
		return ""
	}
	if n.HostName != "" {
		return "host:" + n.HostName
	}
	return fmt.Sprintf("node:%s:%d:%s", n.IP, n.RemotePort, n.RemoteProto)
}

// connectAndVerify spawns openvpn, waits for the handshake, lets the
// tunnel settle, then confirms egress with the HTTPS probe.
func (m *Manager) connectAndVerify(ctx context.Context, n *node.Node) error {
	m.setState(StateConnecting, n.HostName)
	m.mu.Lock()
	m.current = n
	m.mu.Unlock()

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

// StartBlacklistTest launches one serial validation job. The job uses a
// snapshot so nodes newly blacklisted while it runs are not restored by a
// result they did not participate in.
func (m *Manager) StartBlacklistTest() error {
	nodes, err := m.store.LoadNodes()
	if err != nil {
		nodes = nil
	}
	byHost := make(map[string]*node.Node, len(nodes))
	for _, n := range nodes {
		if n == nil || n.HostName == "" {
			continue
		}
		nodeCopy := *n
		byHost[n.HostName] = &nodeCopy
	}

	m.mu.Lock()
	if m.blacklistTest.Running {
		m.mu.Unlock()
		return ErrBlacklistTestRunning
	}
	hosts := make([]string, 0, len(m.blacklist))
	for host := range m.blacklist {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	results := make([]BlacklistTestResult, 0, len(hosts))
	for _, host := range hosts {
		results = append(results, BlacklistTestResult{Host: host, Status: BlacklistTestPending})
	}
	m.blacklistTest = BlacklistTestStatus{
		Running:   true,
		StartedAt: time.Now().Format(time.RFC3339),
		Total:     len(hosts),
		Results:   results,
	}
	taskCtx := m.taskCtx
	m.mu.Unlock()

	logx.Info("blacklist verification started", "count", len(hosts))
	go m.runBlacklistTest(taskCtx, hosts, byHost)
	return nil
}

func (m *Manager) runBlacklistTest(ctx context.Context, hosts []string, byHost map[string]*node.Node) {
	for index, host := range hosts {
		result := BlacklistTestResult{Host: host, Status: BlacklistTestRunning}
		m.setBlacklistTestResult(index, result, false)

		n, ok := byHost[host]
		if !ok {
			result.Status = BlacklistTestSkipped
			result.Code = "node_not_found"
			result.Error = "当前节点池未找到，无法验证"
		} else if err := m.blacklistVerifier(ctx, n); err != nil {
			result.Status = BlacklistTestFailed
			result.Code = "verification_failed"
			result.Error = err.Error()
			logx.Warn("blacklist verification failed", "host", host, "err", err)
		} else {
			result.Status = BlacklistTestPassed
			logx.Info("blacklist verification passed", "host", host)
		}
		result.TestedAt = time.Now().Format(time.RFC3339)
		m.setBlacklistTestResult(index, result, true)
	}

	m.mu.Lock()
	m.blacklistTest.Running = false
	m.blacklistTest.FinishedAt = time.Now().Format(time.RFC3339)
	completed := m.blacklistTest.Completed
	total := m.blacklistTest.Total
	m.mu.Unlock()
	logx.Info("blacklist verification finished", "completed", completed, "total", total)
}

func (m *Manager) setBlacklistTestResult(index int, result BlacklistTestResult, completed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index >= len(m.blacklistTest.Results) {
		return
	}
	m.blacklistTest.Results[index] = result
	if completed {
		m.blacklistTest.Completed++
	}
}

// BlacklistTestStatus returns a copy suitable for API serialization.
func (m *Manager) BlacklistTestStatus() BlacklistTestStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	status := m.blacklistTest
	status.Results = append([]BlacklistTestResult(nil), m.blacklistTest.Results...)
	return status
}

// RestoreAvailableBlacklist removes only nodes that passed the most recent
// completed validation job. The map update and persistence share one lock so
// a concurrent automatic blacklist update cannot be lost.
func (m *Manager) RestoreAvailableBlacklist() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blacklistTest.Running {
		return 0, ErrBlacklistTestRunning
	}
	updated := make(map[string]state.BlacklistEntry, len(m.blacklist))
	for host, entry := range m.blacklist {
		updated[host] = entry
	}
	restored := 0
	for _, result := range m.blacklistTest.Results {
		if result.Status != BlacklistTestPassed {
			continue
		}
		if _, ok := updated[result.Host]; ok {
			delete(updated, result.Host)
			restored++
		}
	}
	if restored == 0 {
		return 0, nil
	}
	if err := m.store.SaveBlacklist(updated); err != nil {
		return 0, err
	}
	m.blacklist = updated
	logx.Info("blacklist nodes restored", "count", restored)
	return restored, nil
}

func (m *Manager) verifyBlacklistedNode(ctx context.Context, n *node.Node) error {
	cfgPath, authPath, cleanup, err := m.prepareProbeFiles(n)
	if err != nil {
		return err
	}
	defer cleanup()

	tun := tunnel.New()
	if err := tun.Start(tunnel.Options{
		ConfigFile:  cfgPath,
		AuthFile:    authPath,
		RouteNoPull: true,
	}); err != nil {
		return err
	}
	defer tun.Stop()
	if err := tun.WaitHandshakeContext(ctx, m.cfg.ConnectTimeout); err != nil {
		return err
	}
	if !sleepCtx(ctx, m.cfg.ProbeSettle) {
		return ctx.Err()
	}
	device := tun.Device()
	if device == "" {
		return errors.New("OpenVPN did not report a tunnel device")
	}

	prober, cleanupRoute, err := health.NewDeviceProber(m.cfg.HealthAddr, m.cfg.ProbeTimeout, device)
	if err != nil {
		return err
	}
	defer cleanupRoute()
	tries := m.cfg.InitialProbeTries
	if tries < 1 {
		tries = 1
	}
	var lastErr error
	for i := 0; i < tries; i++ {
		if err := prober.Check(ctx); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if i+1 < tries && !sleepCtx(ctx, m.cfg.ProbeInterval) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("tunnel health check failed: %w", lastErr)
}

func (m *Manager) prepareProbeFiles(n *node.Node) (cfgPath, authPath string, cleanup func(), err error) {
	safeConfig, err := vpngate.ValidateOpenVPNProfile(n.ConfigText, n.IP)
	if err != nil {
		return "", "", nil, fmt.Errorf("unsafe OpenVPN profile: %w", err)
	}
	if err := os.MkdirAll(m.cfg.DataDir, 0o700); err != nil {
		return "", "", nil, err
	}
	cfgFile, err := os.CreateTemp(m.cfg.DataDir, "blacklist-probe-*.ovpn")
	if err != nil {
		return "", "", nil, err
	}
	cfgPath = cfgFile.Name()
	if err = cfgFile.Chmod(0o600); err == nil {
		_, err = cfgFile.WriteString(safeConfig)
	}
	if closeErr := cfgFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(cfgPath)
		return "", "", nil, err
	}

	authFile, err := os.CreateTemp(m.cfg.DataDir, "blacklist-probe-*.auth")
	if err != nil {
		_ = os.Remove(cfgPath)
		return "", "", nil, err
	}
	authPath = authFile.Name()
	auth := fmt.Sprintf("%s\n%s\n", m.cfg.OpenVPNAuthUser, m.cfg.OpenVPNAuthPass)
	if err = authFile.Chmod(0o600); err == nil {
		_, err = authFile.WriteString(auth)
	}
	if closeErr := authFile.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(cfgPath)
		_ = os.Remove(authPath)
		return "", "", nil, err
	}
	return cfgPath, authPath, func() {
		_ = os.Remove(cfgPath)
		_ = os.Remove(authPath)
	}, nil
}

func (m *Manager) loadBlacklist() {
	bl, err := m.store.LoadBlacklist()
	if err != nil {
		logx.Debug("no blacklist yet", "err", err)
		return
	}
	m.mu.Lock()
	m.blacklist = bl
	m.mu.Unlock()
	logx.Info("blacklist loaded", "count", len(bl))
}

func (m *Manager) isBlacklisted(n *node.Node) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.blacklist[n.HostName]
	return ok
}

func (m *Manager) markBlacklisted(n *node.Node, reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
			if n != nil && wanted[strings.ToUpper(n.CountryShort)] {
				out = append(out, n)
			}
		}
		return m.preferNonHosting(out)
	case "fixed":
		for _, n := range nodes {
			if n != nil && (n.HostName == fixed || n.IP == fixed) {
				return []*node.Node{n}
			}
		}
		return nil
	default:
		out := make([]*node.Node, 0, len(nodes))
		for _, n := range nodes {
			if n != nil {
				out = append(out, n)
			}
		}
		return m.preferNonHosting(out)
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

// wait sleeps d unless the context dies or a background refresh publishes a
// replacement candidate pool.
func (m *Manager) wait(ctx context.Context, d time.Duration) bool {
	ctx = nonNilContext(ctx)
	if d < 0 {
		d = 0
	}
	select {
	case <-ctx.Done():
		return false
	case <-m.candidateCh:
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
