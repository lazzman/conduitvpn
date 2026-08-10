package manager

import (
	"path/filepath"
	"testing"

	"conduitvpn/internal/config"
	"conduitvpn/internal/node"
)

func testManager(t *testing.T, mode, country, node string) *Manager {
	t.Helper()
	cfg := config.Config{DataDir: t.TempDir(), RouteMode: mode, RouteCountry: country, RouteNode: node}
	m := New(cfg)
	// Prefer in-memory config over any persisted file (temp dir is clean).
	return m
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
