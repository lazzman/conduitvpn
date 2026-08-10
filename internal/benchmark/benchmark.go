// Package benchmark measures per-node reachability with concurrent TCP
// connection attempts to the node's remote endpoint.
//
// NOTE: VPNGate nodes are mostly UDP-only, so a TCP connect will time out
// for many of them. That is fine for M1 ranking. M2 will add the real
// handshake-based probe used by the failover logic.
package benchmark

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	"conduitvpn/internal/logx"
	"conduitvpn/internal/node"
)

func Run(ctx context.Context, nodes []*node.Node, concurrency int, timeout time.Duration) {
	if concurrency < 1 {
		concurrency = 1
	}
	jobs := make(chan *node.Node)
	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range jobs {
				probe(ctx, n, timeout)
			}
		}()
	}
	for _, n := range nodes {
		jobs <- n
	}
	close(jobs)
	wg.Wait()
}

func probe(ctx context.Context, n *node.Node, timeout time.Duration) {
	addr := net.JoinHostPort(n.RemoteHost, strconv.Itoa(n.RemotePort))
	start := time.Now()
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		n.Tested = true
		return
	}
	conn.Close()
	n.Tested = true
	n.LatencyMS = int(time.Since(start).Milliseconds())
	logx.Debug("benchmarked", "host", n.HostName, "ms", n.LatencyMS)
}
