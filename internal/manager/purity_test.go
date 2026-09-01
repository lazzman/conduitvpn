package manager

import (
	"context"
	"testing"

	"conduitvpn/internal/config"
	"conduitvpn/internal/purity"
)

func TestSelectCandidatesPrefersNonHosting(t *testing.T) {
	m := testManager(t, "auto", "", "")
	if err := m.store.SavePurity(map[string]purity.Record{
		"1.1.1.1": {Source: "hosting", Hosting: true},
		"1.1.1.2": {Source: "isp"},
		"1.1.1.3": {Source: "hosting", Hosting: true},
	}); err != nil {
		t.Fatal(err)
	}
	got := m.selectCandidates(fakeNodes())
	if len(got) != 4 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].IP != "1.1.1.2" {
		t.Fatalf("first should be residential 1.1.1.2, got %s", got[0].IP)
	}
	if got[1].IP != "1.1.1.1" || got[2].IP != "1.1.1.3" || got[3].IP != "1.1.1.4" {
		t.Fatalf("rest = %s,%s,%s", got[1].IP, got[2].IP, got[3].IP)
	}
}

func TestSelectCandidatesCountryStillPrefersNonHosting(t *testing.T) {
	m := testManager(t, "country", "JP", "")
	if err := m.store.SavePurity(map[string]purity.Record{
		"1.1.1.1": {Hosting: true},
		"1.1.1.2": {Source: "isp"},
	}); err != nil {
		t.Fatal(err)
	}
	got := m.selectCandidates(fakeNodes())
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].IP != "1.1.1.2" || got[1].IP != "1.1.1.1" {
		t.Fatalf("order = %s,%s", got[0].IP, got[1].IP)
	}
}

func TestSelectCandidatesFixedIgnoresHosting(t *testing.T) {
	m := testManager(t, "fixed", "", "vpn-jp-1")
	if err := m.store.SavePurity(map[string]purity.Record{
		"1.1.1.1": {Hosting: true},
		"1.1.1.2": {Source: "isp"},
	}); err != nil {
		t.Fatal(err)
	}
	got := m.selectCandidates(fakeNodes())
	if len(got) != 1 || got[0].HostName != "vpn-jp-1" {
		t.Fatalf("fixed should keep locked hosting node, len=%d", len(got))
	}
}

func TestDemoSeedsPurityWithoutLookup(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), RouteMode: "auto"}
	m := NewDemo(cfg)
	called := 0
	m.purityLookup = func(context.Context, string) (purity.Record, error) {
		called++
		return purity.Record{}, nil
	}
	nodes, err := m.store.LoadNodes()
	if err != nil {
		t.Fatal(err)
	}
	m.enrichPurity(context.Background(), nodes)
	if called != 0 {
		t.Fatalf("demo must not call ipinfo, called=%d", called)
	}
	recs, err := m.store.LoadPurity()
	if err != nil {
		t.Fatal(err)
	}
	if recs["203.0.113.10"].Source != "isp" || !recs["203.0.113.30"].Hosting {
		t.Fatalf("demo purity missing expected records")
	}
}
