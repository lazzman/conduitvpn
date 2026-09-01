package manager

import (
	"context"
	"errors"
	"time"

	"conduitvpn/internal/logx"
	"conduitvpn/internal/node"
	"conduitvpn/internal/purity"
)

func (m *Manager) enrichPurity(ctx context.Context, nodes []*node.Node) {
	if m.demo || m.purityLookup == nil || len(nodes) == 0 {
		return
	}
	go m.lookupPurity(ctx, nodes)
}

func (m *Manager) lookupPurity(ctx context.Context, nodes []*node.Node) {
	pending := make([]string, 0, len(nodes))
	seen := map[string]bool{}
	m.purityMu.Lock()
	cache := m.loadPurityCache()
	now := time.Now()
	for _, n := range nodes {
		ip := n.IP
		if ip == "" || seen[ip] {
			continue
		}
		seen[ip] = true
		if rec, ok := cache[ip]; ok && rec.Fresh(now) {
			continue
		}
		pending = append(pending, ip)
	}
	m.purityMu.Unlock()
	if len(pending) == 0 {
		logx.Info("purity lookup skipped", "cached", len(seen))
		return
	}

	m.purityPending.Store(int32(len(pending)))
	defer m.purityPending.Store(0)
	logx.Info("purity lookup started", "count", len(pending))

	for i, ip := range pending {
		if ctx.Err() != nil {
			logx.Warn("purity lookup canceled", "done", i, "count", len(pending))
			return
		}
		rec, err := m.purityLookup(ctx, ip)
		for errors.Is(err, purity.ErrRateLimited) {
			wait := purity.RetryAfter(err)
			logx.Warn("purity lookup rate limited", "ip", ip, "ttl", wait.String())
			select {
			case <-ctx.Done():
				logx.Warn("purity lookup canceled", "done", i, "count", len(pending))
				return
			case <-time.After(wait):
			}
			rec, err = m.purityLookup(ctx, ip)
		}
		if ctx.Err() != nil {
			return
		}
		checked := time.Now().UTC().Format(time.RFC3339)
		m.purityMu.Lock()
		cache := m.loadPurityCache()
		if err != nil {
			cache[ip] = purity.Record{Error: err.Error(), CheckedAt: checked}
			logx.Warn("purity lookup failed", "ip", ip, "err", err)
		} else {
			rec.CheckedAt = checked
			cache[ip] = rec
			logx.Debug("purity lookup ok", "ip", ip, "source", rec.Source, "hosting", rec.Hosting)
		}
		if err := m.store.SavePurity(cache); err != nil {
			logx.Warn("save purity cache failed", "err", err)
		}
		left := int32(len(pending) - i - 1)
		m.purityPending.Store(left)
		m.purityMu.Unlock()
	}
	logx.Info("purity lookup finished", "count", len(pending))
}

func (m *Manager) loadPurityCache() map[string]purity.Record {
	cache, err := m.store.LoadPurity()
	if err != nil || cache == nil {
		if err != nil {
			logx.Warn("load purity cache failed", "err", err)
		}
		return map[string]purity.Record{}
	}
	return cache
}

func (m *Manager) preferNonHosting(nodes []*node.Node) []*node.Node {
	if len(nodes) < 2 {
		return nodes
	}
	m.purityMu.Lock()
	cache := m.loadPurityCache()
	m.purityMu.Unlock()
	preferred := make([]*node.Node, 0, len(nodes))
	rest := make([]*node.Node, 0, len(nodes))
	for _, n := range nodes {
		rec, ok := cache[n.IP]
		if ok && rec.Error == "" && !rec.Hosting {
			preferred = append(preferred, n)
			continue
		}
		rest = append(rest, n)
	}
	return append(preferred, rest...)
}

func (m *Manager) seedDemoPurity(nodes []*node.Node) {
	recs := map[string]purity.Record{
		"203.0.113.10": {Source: "isp", Country: "JP", Postal: "100-0001", City: "Tokyo", Region: "Tokyo", Org: "Demo ISP", Attrs: []string{"vpn"}, CheckedAt: "demo"},
		"203.0.113.20": {Source: "isp", Country: "KR", Postal: "03121", City: "Seoul", Org: "Demo Broadband", Attrs: []string{"vpn"}, CheckedAt: "demo"},
		"203.0.113.30": {Source: "hosting", Hosting: true, Country: "SG", Postal: "018956", City: "Singapore", Attrs: []string{"vpn", "hosting"}, CheckedAt: "demo"},
		"203.0.113.40": {Source: "business", Country: "DE", Postal: "60311", City: "Frankfurt", Attrs: []string{"vpn"}, CheckedAt: "demo"},
		"203.0.113.50": {Source: "hosting", Hosting: true, Country: "US", Postal: "10001", City: "New York", Attrs: []string{"vpn", "hosting"}, CheckedAt: "demo"},
	}
	if err := m.store.SavePurity(recs); err != nil {
		logx.Warn("save demo purity failed", "err", err)
	}
}
