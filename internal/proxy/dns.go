package proxy

import (
	"context"
	"net"
	"time"
)

// tunnelResolver returns a resolver that queries a fixed public DNS
// server directly. With 方案 B the default route egresses through the
// VPN, so these queries (and the resulting connections) always traverse
// the tunnel instead of depending on the container's resolv.conf chain.
func tunnelResolver(server string) *net.Resolver {
	serverAddr := server
	if _, _, err := net.SplitHostPort(serverAddr); err != nil {
		serverAddr = net.JoinHostPort(serverAddr, "53")
	}
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: 5 * time.Second}
			return d.DialContext(ctx, "udp", serverAddr)
		},
	}
}
