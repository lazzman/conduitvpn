package manager

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"conduitvpn/internal/config"
	"conduitvpn/internal/node"
	"conduitvpn/internal/state"
)

func managerTestVPNGateCSV(host string) []byte {
	profile := "client\ndev tun\nproto udp\nremote 8.8.8.8 1194 udp\nresolv-retry infinite\nnobind\npersist-key\npersist-tun\nauth-nocache\nremote-cert-tls server\ncipher AES-256-GCM\nauth SHA256\nverb 3\n"
	encoded := base64.StdEncoding.EncodeToString([]byte(profile))
	return []byte(fmt.Sprintf("*vpn_servers\n#HostName,IP,Score,Ping,Speed,CountryLong,CountryShort,NumVpnSessions,Uptime,LogType,Operator,Message,OpenVPN_ConfigData_Base64\n%s,8.8.8.8,9000,20,1000000,Japan,JP,1,100,OpenVPN,Test,,%s\n*END\n", host, encoded))
}

func managerSourceTestConfig(t *testing.T, officialURL string) *Manager {
	t.Helper()
	cfg := config.Config{
		DataDir:          t.TempDir(),
		APIURL:           officialURL,
		FetchTimeout:     time.Second,
		FetchInterval:    20 * time.Millisecond,
		MaxScanRows:      10,
		BenchConcurrency: 1,
		BenchTimeout:     time.Millisecond,
	}
	m := New(cfg)
	m.mirrorValidator = func(context.Context, string) error { return nil }
	return m
}

func TestRefreshSourcesOfficialThenMirrorFallback(t *testing.T) {
	var pathsMu sync.Mutex
	var paths []string
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathsMu.Lock()
		paths = append(paths, "official:"+r.URL.Path)
		pathsMu.Unlock()
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	defer official.Close()
	mirrorBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathsMu.Lock()
		paths = append(paths, "bad:"+r.URL.Path)
		pathsMu.Unlock()
		_, _ = w.Write([]byte("not csv"))
	}))
	defer mirrorBad.Close()
	mirrorGood := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathsMu.Lock()
		paths = append(paths, "good:"+r.URL.Path)
		pathsMu.Unlock()
		_, _ = w.Write(managerTestVPNGateCSV("mirror-good"))
	}))
	defer mirrorGood.Close()

	m := managerSourceTestConfig(t, official.URL)
	m.mirrors = []string{mirrorBad.URL, mirrorGood.URL}
	got := m.refreshNodes(context.Background(), true)
	if len(got) != 1 || got[0].HostName != "mirror-good" {
		t.Fatalf("nodes = %#v, want mirror-good", got)
	}
	status := m.VPNGateSourceStatus()
	if status.CurrentSource != mirrorGood.URL+mirrorAPIPath || len(status.Attempts) != 3 {
		t.Fatalf("source status = %+v", status)
	}
	if !status.Attempts[2].OK || status.Attempts[0].OK || status.Attempts[1].OK {
		t.Fatalf("attempts = %#v", status.Attempts)
	}
	pathsMu.Lock()
	gotPaths := append([]string{}, paths...)
	pathsMu.Unlock()
	wantPaths := []string{"official:/", "bad:" + mirrorAPIPath, "good:" + mirrorAPIPath}
	if strings.Join(gotPaths, "\n") != strings.Join(wantPaths, "\n") {
		t.Fatalf("request order = %#v, want %#v", gotPaths, wantPaths)
	}
	if status.Refreshing {
		t.Fatal("refreshing remained true after refresh")
	}
}

func TestMirrorEndpointAlwaysUsesVPNGateAPIPath(t *testing.T) {
	if got := mirrorEndpoint("HTTP://Example.com:80/cn/"); got != "http://example.com/api/iphone/" {
		t.Fatalf("mirror endpoint = %q", got)
	}
}

func TestRefreshSourcesStopsAfterOfficialSuccess(t *testing.T) {
	var mirrorRequests int
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(managerTestVPNGateCSV("official-node"))
	}))
	defer official.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mirrorRequests++
		_, _ = w.Write(managerTestVPNGateCSV("mirror-node"))
	}))
	defer mirror.Close()
	m := managerSourceTestConfig(t, official.URL)
	m.mirrors = []string{mirror.URL}
	got := m.refreshNodes(context.Background(), false)
	if len(got) != 1 || got[0].HostName != "official-node" {
		t.Fatalf("nodes = %#v, want official-node", got)
	}
	if mirrorRequests != 0 {
		t.Fatalf("mirror requests = %d, want 0 after official success", mirrorRequests)
	}
	status := m.VPNGateSourceStatus()
	if len(status.Attempts) != 1 || !status.Attempts[0].OK || status.CurrentSource != official.URL {
		t.Fatalf("status = %+v", status)
	}
}

func TestForegroundRefreshDoesNotLeaveConnectedStateFetching(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(managerTestVPNGateCSV("connected-refresh"))
	}))
	defer server.Close()
	m := managerSourceTestConfig(t, server.URL)
	m.setState(StateConnected, "existing-node")
	if got := m.refreshNodes(context.Background(), true); len(got) != 1 {
		t.Fatalf("refresh nodes = %#v", got)
	}
	if snap := m.Snapshot(); snap.State != string(StateConnected) {
		t.Fatalf("foreground refresh changed connected state: %+v", snap)
	}
}

func TestSourceStatusReportsRefreshingDuringNetworkRound(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = w.Write(managerTestVPNGateCSV("blocked-refresh"))
	}))
	defer server.Close()
	m := managerSourceTestConfig(t, server.URL)
	done := make(chan struct{})
	go func() {
		m.refreshNodes(context.Background(), false)
		close(done)
	}()
	<-started
	status := m.VPNGateSourceStatus()
	if !status.Refreshing || status.Attempts == nil {
		t.Fatalf("in-flight status = %+v", status)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh did not finish")
	}
	if m.VPNGateSourceStatus().Refreshing {
		t.Fatal("refreshing remained true")
	}
}

func TestMirrorConfigChangeAppliesOnNextRefreshRound(t *testing.T) {
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	var mu sync.Mutex
	var requests []string
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		count := len(requests)
		mu.Unlock()
		if count == 1 {
			close(firstStarted)
			<-firstRelease
			http.Error(w, "first round failed", http.StatusBadGateway)
			return
		}
		http.Error(w, "official unavailable", http.StatusBadGateway)
	}))
	defer official.Close()
	oldMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "old mirror should be used only in first round", http.StatusBadGateway)
	}))
	defer oldMirror.Close()
	newMirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, "new:"+r.URL.Path)
		mu.Unlock()
		_, _ = w.Write(managerTestVPNGateCSV("new-mirror"))
	}))
	defer newMirror.Close()
	m := managerSourceTestConfig(t, official.URL)
	m.mirrors = []string{oldMirror.URL}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		m.refreshLoop(ctx)
		close(done)
	}()
	m.requestRefresh()
	<-firstStarted
	if _, err := m.SetVPNGateMirrors(ctx, newMirror.URL); err != nil {
		t.Fatal(err)
	}
	close(firstRelease)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		found := false
		for _, path := range requests {
			if path == "new:"+mirrorAPIPath {
				found = true
			}
		}
		mu.Unlock()
		if found {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	gotRequests := append([]string{}, requests...)
	mu.Unlock()
	cancel()
	<-done
	if len(gotRequests) < 2 || !strings.Contains(strings.Join(gotRequests, "\n"), "new:"+mirrorAPIPath) {
		t.Fatalf("requests = %#v, expected next-round new mirror", gotRequests)
	}
}

func TestRefreshRejectsRedirectAndPreservesCandidatePool(t *testing.T) {
	redirectTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(managerTestVPNGateCSV("redirect-target"))
	}))
	defer redirectTarget.Close()
	official := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectTarget.URL, http.StatusFound)
	}))
	defer official.Close()
	m := managerSourceTestConfig(t, official.URL)
	old := []*node.Node{{HostName: "cached-node", IP: "8.8.8.8"}}
	m.setCandidatePool(old)
	m.setState(StateConnected, "cached-node")
	got := m.refreshNodes(context.Background(), false)
	if len(got) != 1 || got[0].HostName != "cached-node" {
		t.Fatalf("fallback nodes = %#v", got)
	}
	if pool := m.candidateSnapshot(); len(pool) != 1 || pool[0].HostName != "cached-node" {
		t.Fatalf("candidate pool was replaced: %#v", pool)
	}
	if snap := m.Snapshot(); snap.State != string(StateConnected) {
		t.Fatalf("state changed during background failure: %+v", snap)
	}
	status := m.VPNGateSourceStatus()
	if len(status.Attempts) != 1 || status.Attempts[0].OK || !strings.Contains(status.Attempts[0].Error, "unexpected status 302") {
		t.Fatalf("redirect attempt = %#v", status.Attempts)
	}
}

func TestRefreshRequestSequenceCoalescesAndDoesNotLosePending(t *testing.T) {
	m := testManager(t, "auto", "", "")
	m.requestRefresh()
	m.requestRefresh()
	if !m.takeRefreshRequest() {
		t.Fatal("expected pending refresh")
	}
	// A request arriving after the first one was consumed must schedule the
	// next round independently.
	m.requestRefresh()
	if !m.takeRefreshRequest() {
		t.Fatal("pending refresh was lost")
	}
	if m.takeRefreshRequest() {
		t.Fatal("unexpected extra refresh")
	}
}

func TestRefreshLoopHonorsTickerAndKeepsConnectedState(t *testing.T) {
	var requestsMu sync.Mutex
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestsMu.Lock()
		requests++
		requestsMu.Unlock()
		_, _ = w.Write(managerTestVPNGateCSV("ticker-node"))
	}))
	defer server.Close()
	m := managerSourceTestConfig(t, server.URL)
	m.cfg.FetchInterval = 15 * time.Millisecond
	m.setState(StateConnected, "existing-node")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		m.refreshLoop(ctx)
		close(done)
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		requestsMu.Lock()
		count := requests
		requestsMu.Unlock()
		if count >= 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("refresh loop did not stop")
	}
	requestsMu.Lock()
	count := requests
	requestsMu.Unlock()
	if count < 2 {
		t.Fatalf("ticker requests = %d, want at least 2", count)
	}
	if snap := m.Snapshot(); snap.State != string(StateConnected) {
		t.Fatalf("background ticker changed state: %+v", snap)
	}
}

func TestVPNGateSourceStatusUsesEmptyArrays(t *testing.T) {
	m := testManager(t, "auto", "", "")
	data, err := json.Marshal(m.VPNGateSourceStatus())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"mirrors":[]`) || !strings.Contains(string(data), `"attempts":[]`) {
		t.Fatalf("status JSON = %s", data)
	}
}

func TestSetVPNGateMirrorsPartiallyFiltersDNSFailures(t *testing.T) {
	m := testManager(t, "auto", "", "")
	m.mirrorValidator = func(_ context.Context, origin string) error {
		if strings.Contains(origin, "blocked") {
			return errors.New("mirror host resolves to non-public IP")
		}
		return nil
	}
	update, err := m.SetVPNGateMirrors(context.Background(), "http://ok.example/path\nhttp://blocked.example/path")
	if err != nil {
		t.Fatalf("partial update failed: %v", err)
	}
	if len(update.Mirrors) != 1 || update.Mirrors[0] != "http://ok.example" || len(update.Issues) != 1 {
		t.Fatalf("update = %+v", update)
	}
	status := m.VPNGateSourceStatus()
	if len(status.Mirrors) != 1 || status.Mirrors[0] != "http://ok.example" {
		t.Fatalf("saved mirrors = %#v", status.Mirrors)
	}
}

func TestSetVPNGateMirrorsAllInvalidLeavesOldConfiguration(t *testing.T) {
	m := testManager(t, "auto", "", "")
	m.mirrorValidator = func(context.Context, string) error { return errors.New("DNS failure") }
	if _, err := m.SetVPNGateMirrors(context.Background(), "http://old.example"); err == nil {
		t.Fatal("expected all-invalid update to fail")
	}
	if _, err := m.SetVPNGateMirrors(context.Background(), "http://new.example"); err == nil {
		t.Fatal("expected second all-invalid update to fail")
	}
	if got := m.VPNGateSourceStatus().Mirrors; len(got) != 0 {
		t.Fatalf("old configuration unexpectedly changed: %#v", got)
	}
}

func TestVPNGateMirrorsReloadAcrossManagerRestart(t *testing.T) {
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, Demo: true}
	m := NewDemo(cfg)
	if _, err := m.SetVPNGateMirrors(context.Background(), "http://mirror.example/cn/\nhttps://mirror.example:443"); err != nil {
		t.Fatal(err)
	}
	m2 := New(config.Config{DataDir: dir})
	got := m2.VPNGateSourceStatus().Mirrors
	want := []string{"http://mirror.example", "https://mirror.example"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("reloaded mirrors = %#v, want %#v", got, want)
	}
}

func TestDemoMirrorSaveSkipsNetworkValidation(t *testing.T) {
	m := NewDemo(config.Config{DataDir: t.TempDir(), Demo: true})
	update, err := m.SetVPNGateMirrors(context.Background(), "http://127.0.0.1:9/cn/")
	if err != nil || len(update.Mirrors) != 1 || update.Mirrors[0] != "http://127.0.0.1:9" {
		t.Fatalf("demo mirror update = %+v, err=%v", update, err)
	}
}

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
