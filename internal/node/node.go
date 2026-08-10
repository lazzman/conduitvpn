// Package node defines the VPNGate node model and ranking helpers.
package node

import (
	"fmt"
	"sort"
)

type Node struct {
	HostName     string `json:"host_name"`
	IP           string `json:"ip"`
	Score        int    `json:"score"`
	Ping         int    `json:"ping"`
	Speed        int    `json:"speed"`
	CountryLong  string `json:"country_long"`
	CountryShort string `json:"country_short"`
	Sessions     int    `json:"sessions"`
	Uptime       int64  `json:"uptime"`
	LogType      string `json:"log_type"`
	Operator     string `json:"operator"`
	Message      string `json:"message"`

	// Derived from the OpenVPN config.
	ConfigText  string `json:"config_text"`
	RemoteHost  string `json:"remote_host"`
	RemotePort  int    `json:"remote_port"`
	RemoteProto string `json:"remote_proto"`

	// Measured locally.
	LatencyMS int  `json:"latency_ms"` // 0 = unknown / unreachable
	Tested    bool `json:"tested"`
}

// SortByScore returns nodes ordered by VPNGate score, highest first.
func SortByScore(nodes []*Node) []*Node {
	out := append([]*Node(nil), nodes...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// TopByLatency returns the k reachable nodes with the lowest measured
// latency, best first.
func TopByLatency(nodes []*Node, k int) []*Node {
	tested := make([]*Node, 0, len(nodes))
	for _, n := range nodes {
		if n.Tested && n.LatencyMS > 0 {
			tested = append(tested, n)
		}
	}
	sort.SliceStable(tested, func(i, j int) bool { return tested[i].LatencyMS < tested[j].LatencyMS })
	if len(tested) > k {
		tested = tested[:k]
	}
	return tested
}

func (n *Node) RemoteAddr() string {
	if n.RemotePort > 0 {
		return fmt.Sprintf("%s:%d", n.RemoteHost, n.RemotePort)
	}
	return n.RemoteHost
}
