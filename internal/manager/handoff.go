package manager

import (
	"context"
	"errors"
	"fmt"

	"conduitvpn/internal/egress"
	"conduitvpn/internal/health"
	"conduitvpn/internal/logx"
	"conduitvpn/internal/node"
	"conduitvpn/internal/tunnel"
)

func connectPlan(live bool, currentHost, nextHost string) string {
	if live && currentHost != "" && currentHost == nextHost {
		return "reuse"
	}
	if live {
		return "handoff"
	}
	return "cold"
}

func (m *Manager) liveTunnel() bool {
	return m.tun != nil && m.tun.Running()
}

func (m *Manager) currentNode() *node.Node {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

func (m *Manager) setCurrent(n *node.Node) {
	m.mu.Lock()
	m.current = n
	m.mu.Unlock()
}

func (m *Manager) setTarget(n *node.Node) {
	m.mu.Lock()
	m.target = n
	m.mu.Unlock()
}

func (m *Manager) clearTarget() {
	m.mu.Lock()
	m.target = nil
	m.mu.Unlock()
}

func (m *Manager) cachedCandidates() ([]*node.Node, bool) {
	if latest := m.candidateSnapshot(); len(latest) > 0 {
		out := m.selectCandidates(latest)
		if len(out) > 0 {
			return out, true
		}
	}
	cached, err := m.store.LoadNodes()
	if err != nil || len(cached) == 0 {
		return nil, false
	}
	return m.selectCandidates(cached), true
}

func (m *Manager) connectAndVerify(ctx context.Context, n *node.Node) error {
	if n == nil {
		return errors.New("nil node")
	}
	cur := m.currentNode()
	currentHost := ""
	if cur != nil {
		currentHost = cur.HostName
	}
	switch connectPlan(m.liveTunnel(), currentHost, n.HostName) {
	case "reuse":
		m.setState(StateConnected, n.HostName)
		logx.Info("already on target node", "host", n.HostName)
		return nil
	case "handoff":
		return m.switchTo(ctx, n)
	default:
		if m.tun != nil {
			m.stopTunnel()
		}
		return m.coldStart(ctx, n)
	}
}

func (m *Manager) coldStart(ctx context.Context, n *node.Node) error {
	m.setState(StateConnecting, n.HostName)
	m.setCurrent(n)

	cfgPath, authPath, err := m.prepareFiles(n)
	if err != nil {
		return fmt.Errorf("prepare files: %w", err)
	}

	tun := tunnel.New()
	if err := tun.Start(tunnel.Options{
		ConfigFile:  cfgPath,
		AuthFile:    authPath,
		RouteNoPull: m.cfg.NetworkMode == "host",
	}); err != nil {
		return fmt.Errorf("spawn openvpn: %w", err)
	}
	m.tun = tun
	if err := tun.WaitHandshakeContext(ctx, m.cfg.ConnectTimeout); err != nil {
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

	tries := m.cfg.InitialProbeTries
	if tries < 1 {
		tries = 1
	}
	var lastErr error
	for i := 0; i < tries; i++ {
		probeErr := m.prober.Check(ctx)
		if probeErr == nil {
			m.setState(StateConnected, n.HostName)
			logx.Info("tunnel healthy", "host", n.HostName, "remote", n.RemoteAddr())
			return nil
		}
		lastErr = probeErr
		logx.Warn("initial probe failed", "host", n.HostName, "try", i+1, "err", probeErr)
		if !sleepCtx(ctx, m.cfg.ProbeInterval) {
			m.stopTunnel()
			return ctx.Err()
		}
	}
	m.stopTunnel()
	return fmt.Errorf("initial probe degraded: %v", lastErr)
}

// switchTo brings up a route-nopull standby tunnel and cuts over only after
// handshake and health checks succeed. On failure the serving tunnel is kept.
func (m *Manager) switchTo(ctx context.Context, n *node.Node) error {
	from := ""
	if cur := m.currentNode(); cur != nil {
		from = cur.HostName
	}
	m.setTarget(n)
	m.setState(StateConnecting, n.HostName)
	logx.Info("preparing tunnel handoff", "from", from, "to", n.HostName)

	fail := func(err error) error {
		m.clearTarget()
		if cur := m.currentNode(); cur != nil && m.liveTunnel() {
			m.setState(StateConnected, cur.HostName)
		}
		logx.Warn("standby failed, keeping current tunnel", "host", n.HostName, "err", err)
		return err
	}

	cfgPath, authPath, err := m.prepareFiles(n)
	if err != nil {
		return fail(fmt.Errorf("prepare files: %w", err))
	}

	next := tunnel.New()
	if err := next.Start(tunnel.Options{
		ConfigFile:  cfgPath,
		AuthFile:    authPath,
		RouteNoPull: true,
	}); err != nil {
		return fail(fmt.Errorf("spawn standby openvpn: %w", err))
	}

	failStandby := func(err error) error {
		_ = next.Stop()
		return fail(err)
	}

	if err := next.WaitHandshakeContext(ctx, m.cfg.ConnectTimeout); err != nil {
		return failStandby(err)
	}
	logx.Info("standby handshake ok", "host", n.HostName, "device", next.Device())

	if !sleepCtx(ctx, m.cfg.ProbeSettle) {
		return failStandby(ctx.Err())
	}
	device := next.Device()
	if device == "" {
		return failStandby(errors.New("OpenVPN did not report a tunnel device"))
	}

	prober, cleanupRoute, err := health.NewDeviceProber(m.cfg.HealthAddr, m.cfg.ProbeTimeout, device)
	if err != nil {
		return failStandby(err)
	}
	defer cleanupRoute()

	tries := m.cfg.InitialProbeTries
	if tries < 1 {
		tries = 1
	}
	var lastErr error
	healthy := false
	for i := 0; i < tries; i++ {
		probeErr := prober.Check(ctx)
		if probeErr == nil {
			healthy = true
			break
		}
		lastErr = probeErr
		logx.Warn("standby probe failed", "host", n.HostName, "try", i+1, "err", probeErr)
		if i+1 < tries && !sleepCtx(ctx, m.cfg.ProbeInterval) {
			return failStandby(ctx.Err())
		}
	}
	if !healthy {
		return failStandby(fmt.Errorf("initial probe degraded: %v", lastErr))
	}
	logx.Info("standby probe ok", "host", n.HostName)

	logx.Info("cutting over", "from", from, "to", n.HostName, "device", device)
	if err := m.cutOver(next); err != nil {
		return failStandby(err)
	}
	m.adoptTunnel(next, n)
	m.setState(StateConnected, n.HostName)
	logx.Info("handoff complete", "host", n.HostName, "remote", n.RemoteAddr())
	return nil
}

func (m *Manager) cutOver(next *tunnel.Tunnel) error {
	device := next.Device()
	if device == "" {
		return errors.New("OpenVPN did not report a tunnel device")
	}
	if m.cfg.NetworkMode == "host" {
		return m.egress.Switch(device)
	}
	return egress.ReplaceDefaultDev(device)
}

func (m *Manager) adoptTunnel(next *tunnel.Tunnel, n *node.Node) {
	old := m.tun
	m.tun = next
	m.mu.Lock()
	m.current = n
	m.target = nil
	m.mu.Unlock()
	if old != nil {
		_ = old.Stop()
	}
	if m.cfg.NetworkMode == "container" {
		if err := egress.ReplaceDefaultDev(next.Device()); err != nil {
			logx.Warn("reassert default route after handoff", "device", next.Device(), "err", err)
		}
	}
}
