package manager

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"conduitvpn/internal/config"
	"conduitvpn/internal/node"
	"conduitvpn/internal/state"
)

func testManager(t *testing.T, mode, country, node string) *Manager {
	t.Helper()
	cfg := config.Config{DataDir: t.TempDir(), RouteMode: mode, RouteCountry: country, RouteNode: node}
	m := New(cfg)
	// Prefer in-memory config over any persisted file (temp dir is clean).
	return m
}

func TestNewDemoSeedsInteractiveNodes(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), RouteMode: "auto"}
	m := NewDemo(cfg)
	snap := m.Snapshot()
	if snap.State != string(StateConnected) || snap.CurrentNode == nil {
		t.Fatalf("demo snapshot = %+v", snap)
	}
	nodes, err := m.store.LoadNodes()
	if err != nil || len(nodes) < 4 {
		t.Fatalf("demo nodes = %d, err = %v", len(nodes), err)
	}
	before := nodes[0].LatencyMS
	m.TriggerFetch()
	nodes, err = m.store.LoadNodes()
	if err != nil || nodes[0].LatencyMS == before {
		t.Fatalf("demo refresh did not update nodes: before=%d after=%d err=%v", before, nodes[0].LatencyMS, err)
	}
	if err := m.SetRouteConfig("fixed", "", "demo-newyork-01"); err != nil {
		t.Fatal(err)
	}
	if got := m.Snapshot().CurrentNode; got == nil || got.HostName != "demo-newyork-01" {
		t.Fatalf("fixed demo node = %+v", got)
	}
}

func fakeNodes() []*node.Node {
	mk := func(host, ip, cc string) *node.Node {
		return &node.Node{HostName: host, IP: ip, CountryShort: cc, Tested: true, LatencyMS: 50}
	}
	return []*node.Node{
		mk("vpn-jp-1", "1.1.1.1", "JP"),
		mk("vpn-jp-2", "1.1.1.2", "JP"),
		mk("vpn-kr-1", "1.1.1.3", "KR"),
		mk("vpn-us-1", "1.1.1.4", "US"),
	}
}

func TestSelectCandidatesAuto(t *testing.T) {
	m := testManager(t, "auto", "", "")
	got := m.selectCandidates(fakeNodes())
	if len(got) != 4 {
		t.Fatalf("auto mode should keep all nodes, got %d", len(got))
	}
}

func TestSelectCandidatesCountry(t *testing.T) {
	m := testManager(t, "country", "jp", "")
	got := m.selectCandidates(fakeNodes())
	if len(got) != 2 {
		t.Fatalf("country JP should yield 2 nodes, got %d", len(got))
	}
	for _, n := range got {
		if n.CountryShort != "JP" {
			t.Fatalf("unexpected country %s", n.CountryShort)
		}
	}

	// comma-separated list
	m2 := testManager(t, "country", "JP,KR", "")
	if got := m2.selectCandidates(fakeNodes()); len(got) != 3 {
		t.Fatalf("JP,KR should yield 3 nodes, got %d", len(got))
	}

	// no match → empty
	m3 := testManager(t, "country", "DE", "")
	if got := m3.selectCandidates(fakeNodes()); len(got) != 0 {
		t.Fatalf("DE should yield 0 nodes, got %d", len(got))
	}
}

func TestSelectCandidatesFixed(t *testing.T) {
	m := testManager(t, "fixed", "", "vpn-kr-1")
	got := m.selectCandidates(fakeNodes())
	if len(got) != 1 || got[0].HostName != "vpn-kr-1" {
		t.Fatalf("fixed mode should lock to vpn-kr-1, got %v", got)
	}

	// match by IP
	m2 := testManager(t, "fixed", "", "1.1.1.4")
	got = m2.selectCandidates(fakeNodes())
	if len(got) != 1 || got[0].HostName != "vpn-us-1" {
		t.Fatalf("fixed by IP failed, got %v", got)
	}

	// unknown node → empty
	m3 := testManager(t, "fixed", "", "vpn-zz-9")
	if got := m3.selectCandidates(fakeNodes()); len(got) != 0 {
		t.Fatalf("unknown fixed node should yield 0, got %d", len(got))
	}
}

func TestSetRouteConfigValidation(t *testing.T) {
	m := testManager(t, "auto", "", "")
	if err := m.SetRouteConfig("bogus", "", ""); err == nil {
		t.Fatal("bogus mode should be rejected")
	}
	if err := m.SetRouteConfig("country", "", ""); err == nil {
		t.Fatal("country mode without code should be rejected")
	}
	if err := m.SetRouteConfig("fixed", "", ""); err == nil {
		t.Fatal("fixed mode without node should be rejected")
	}
	if err := m.SetRouteConfig("country", "jp", ""); err != nil {
		t.Fatalf("valid country mode rejected: %v", err)
	}
	mode, country, _ := m.RouteConfig()
	if mode != "country" || country != "JP" {
		t.Fatalf("route config not applied: %s/%s", mode, country)
	}
	// persisted
	loaded, err := m.store.LoadRoute()
	if err != nil || loaded.Mode != "country" || loaded.Country != "JP" {
		t.Fatalf("route not persisted: %+v err=%v", loaded, err)
	}
}

func TestRouteConfigFileWinsOverEnv(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, RouteMode: "auto"}
	m := New(cfg)
	if err := m.SetRouteConfig("fixed", "", "vpn-jp-1"); err != nil {
		t.Fatal(err)
	}
	// A second manager instance should pick up the persisted fixed mode.
	m2 := New(config.Config{DataDir: dir, RouteMode: "auto"})
	mode, _, node := m2.RouteConfig()
	if mode != "fixed" || node != "vpn-jp-1" {
		t.Fatalf("persisted route should win: %s/%s", mode, node)
	}
	_ = filepath.Join(dir) // keep filepath import meaningful
}

func TestPrepareFilesRevalidatesCachedProfile(t *testing.T) {
	m := testManager(t, "auto", "", "")
	n := &node.Node{
		HostName: "vpn-test",
		IP:       "8.8.8.8",
		ConfigText: "client\n" +
			"dev tun\n" +
			"remote 8.8.8.8 1194 udp\n" +
			"script-security 2\n",
	}
	if _, _, err := m.prepareFiles(n); err == nil {
		t.Fatal("unsafe cached profile should not be written")
	}
}

func setupBlacklistTestManager(t *testing.T, hosts ...string) *Manager {
	t.Helper()
	m := testManager(t, "auto", "", "")
	if err := m.store.SaveNodes(fakeNodes()); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	for _, host := range hosts {
		m.blacklist[host] = state.BlacklistEntry{Reason: "test", MarkedAt: "2026-01-01T00:00:00Z"}
	}
	m.mu.Unlock()
	if err := m.store.SaveBlacklist(m.blacklist); err != nil {
		t.Fatal(err)
	}
	return m
}

func waitBlacklistTest(t *testing.T, m *Manager) BlacklistTestStatus {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status := m.BlacklistTestStatus()
		if !status.Running {
			return status
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("blacklist test did not finish")
	return BlacklistTestStatus{}
}

func TestBlacklistTestSerialAndMissingNode(t *testing.T) {
	m := setupBlacklistTestManager(t, "vpn-jp-1", "vpn-kr-1", "gone-node")
	var mu sync.Mutex
	var verified []string
	m.blacklistVerifier = func(_ context.Context, n *node.Node) error {
		mu.Lock()
		verified = append(verified, n.HostName)
		mu.Unlock()
		if n.HostName == "vpn-kr-1" {
			return errors.New("handshake timeout")
		}
		return nil
	}
	if err := m.StartBlacklistTest(); err != nil {
		t.Fatal(err)
	}
	status := waitBlacklistTest(t, m)
	if status.Total != 3 || status.Completed != 3 || len(status.Results) != 3 {
		t.Fatalf("unexpected status: %+v", status)
	}
	if got := status.Results[0]; got.Host != "gone-node" || got.Status != BlacklistTestSkipped {
		t.Fatalf("missing-node result = %+v", got)
	}
	if got := status.Results[1]; got.Host != "vpn-jp-1" || got.Status != BlacklistTestPassed {
		t.Fatalf("successful result = %+v", got)
	}
	if got := status.Results[2]; got.Host != "vpn-kr-1" || got.Status != BlacklistTestFailed {
		t.Fatalf("failed result = %+v", got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(verified) != 2 || verified[0] != "vpn-jp-1" || verified[1] != "vpn-kr-1" {
		t.Fatalf("verification order = %v", verified)
	}
}

func TestBlacklistTestRejectsConcurrentRun(t *testing.T) {
	m := setupBlacklistTestManager(t, "vpn-jp-1")
	started := make(chan struct{})
	release := make(chan struct{})
	m.blacklistVerifier = func(_ context.Context, _ *node.Node) error {
		close(started)
		<-release
		return nil
	}
	if err := m.StartBlacklistTest(); err != nil {
		t.Fatal(err)
	}
	<-started
	if err := m.StartBlacklistTest(); !errors.Is(err, ErrBlacklistTestRunning) {
		t.Fatalf("concurrent start error = %v", err)
	}
	close(release)
	_ = waitBlacklistTest(t, m)
}

func TestRestoreAvailableBlacklistOnlyRestoresPassed(t *testing.T) {
	m := setupBlacklistTestManager(t, "vpn-jp-1", "vpn-kr-1")
	m.mu.Lock()
	m.blacklistTest = BlacklistTestStatus{Results: []BlacklistTestResult{
		{Host: "vpn-jp-1", Status: BlacklistTestPassed},
		{Host: "vpn-kr-1", Status: BlacklistTestFailed},
		{Host: "gone-node", Status: BlacklistTestPassed},
	}}
	m.mu.Unlock()
	restored, err := m.RestoreAvailableBlacklist()
	if err != nil || restored != 1 {
		t.Fatalf("restore = %d, %v", restored, err)
	}
	m.mu.Lock()
	_, jp := m.blacklist["vpn-jp-1"]
	_, kr := m.blacklist["vpn-kr-1"]
	m.mu.Unlock()
	if jp || !kr {
		t.Fatalf("blacklist after restore: jp=%t kr=%t", jp, kr)
	}
	persisted, err := m.store.LoadBlacklist()
	if err != nil || len(persisted) != 1 || persisted["vpn-kr-1"].Reason != "test" {
		t.Fatalf("persisted blacklist = %+v, err = %v", persisted, err)
	}
}
