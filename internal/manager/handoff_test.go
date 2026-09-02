package manager

import (
	"testing"

	"conduitvpn/internal/node"
)

func TestConnectPlan(t *testing.T) {
	cases := []struct {
		live          bool
		current, next string
		want          string
	}{
		{true, "vpn-a", "vpn-a", "reuse"},
		{true, "vpn-a", "vpn-b", "handoff"},
		{false, "vpn-a", "vpn-b", "cold"},
		{false, "", "vpn-b", "cold"},
		{true, "", "vpn-b", "handoff"},
	}
	for _, tc := range cases {
		if got := connectPlan(tc.live, tc.current, tc.next); got != tc.want {
			t.Fatalf("connectPlan(%v, %q, %q) = %q, want %q", tc.live, tc.current, tc.next, got, tc.want)
		}
	}
}

func TestSnapshotIncludesTargetNode(t *testing.T) {
	m := testManager(t, "auto", "", "")
	cur := &node.Node{HostName: "vpn-a", IP: "203.0.113.10", ConfigText: "SECRET-A"}
	tgt := &node.Node{HostName: "vpn-b", IP: "203.0.113.20", ConfigText: "SECRET-B"}
	m.mu.Lock()
	m.current = cur
	m.target = tgt
	m.state = StateConnecting
	m.stateDetail = tgt.HostName
	m.mu.Unlock()

	snap := m.Snapshot()
	if snap.State != string(StateConnecting) {
		t.Fatalf("state = %s", snap.State)
	}
	if snap.CurrentNode == nil || snap.CurrentNode.HostName != "vpn-a" {
		t.Fatalf("current = %+v", snap.CurrentNode)
	}
	if snap.TargetNode == nil || snap.TargetNode.HostName != "vpn-b" {
		t.Fatalf("target = %+v", snap.TargetNode)
	}
}
