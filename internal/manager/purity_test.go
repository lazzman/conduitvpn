package manager

import (
	"context"
	"testing"
	"time"

	"conduitvpn/internal/config"
	"conduitvpn/internal/node"
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
		t.Fatalf("demo must not call purity lookup, called=%d", called)
	}
	recs, err := m.store.LoadPurity()
	if err != nil {
		t.Fatal(err)
	}
	if recs["203.0.113.10"].Source != "isp" || !recs["203.0.113.30"].Hosting {
		t.Fatalf("demo purity missing expected records")
	}
}

func TestLookupPuritySkipsFreshCache(t *testing.T) {
	m := testManager(t, "auto", "", "")
	now := time.Now().UTC()
	fresh := now.Add(-time.Hour).Format(time.RFC3339)
	stale := now.Add(-25 * time.Hour).Format(time.RFC3339)
	errFresh := now.Add(-5 * time.Minute).Format(time.RFC3339)
	if err := m.store.SavePurity(map[string]purity.Record{
		"1.1.1.1": {Source: "isp", CheckedAt: fresh},
		"1.1.1.2": {Source: "hosting", Hosting: true, CheckedAt: stale},
		"1.1.1.3": {Error: "ip-api rate limited", CheckedAt: errFresh},
	}); err != nil {
		t.Fatal(err)
	}
	var called []string
	m.purityLookup = func(_ context.Context, ip string) (purity.Record, error) {
		called = append(called, ip)
		return purity.Record{Source: "isp", Country: "XX"}, nil
	}
	m.lookupPurity(context.Background(), fakeNodes())
	if len(called) != 2 || called[0] != "1.1.1.2" || called[1] != "1.1.1.4" {
		t.Fatalf("looked up %v, want 1.1.1.2 then 1.1.1.4", called)
	}
	recs, err := m.store.LoadPurity()
	if err != nil {
		t.Fatal(err)
	}
	if recs["1.1.1.1"].Source != "isp" || recs["1.1.1.3"].Error == "" {
		t.Fatalf("fresh cache mutated: %+v", recs)
	}
	if recs["1.1.1.2"].Source != "isp" || recs["1.1.1.2"].Error != "" {
		t.Fatalf("stale record not refreshed: %+v", recs["1.1.1.2"])
	}
	if recs["1.1.1.4"].Country != "XX" {
		t.Fatalf("missing ip not filled: %+v", recs["1.1.1.4"])
	}
}

func TestLookupPurityRetriesRateLimitWithoutCaching(t *testing.T) {
	m := testManager(t, "auto", "", "")
	calls := 0
	m.purityLookup = func(_ context.Context, ip string) (purity.Record, error) {
		calls++
		if calls < 3 {
			return purity.Record{}, &purity.RateLimitError{RetryAfter: time.Millisecond}
		}
		return purity.Record{Source: "isp", Country: "JP"}, nil
	}
	nodes := []*node.Node{{HostName: "vpn-1", IP: "8.8.8.8", CountryShort: "US", Tested: true}}
	m.lookupPurity(context.Background(), nodes)
	if calls != 3 {
		t.Fatalf("calls=%d, want 3", calls)
	}
	recs, err := m.store.LoadPurity()
	if err != nil {
		t.Fatal(err)
	}
	got := recs["8.8.8.8"]
	if got.Error != "" || got.Source != "isp" || got.Country != "JP" {
		t.Fatalf("cached %+v, rate limit must not be stored as error", got)
	}
}
