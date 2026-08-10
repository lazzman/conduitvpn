package proxy

import (
	"io"
	"net"
	"sync"
)

// relay copies bytes in both directions until either side closes.
// Each direction signals EOF to its peer with CloseWrite so half-closed
// streams (e.g. HTTP responses after request end) terminate cleanly.
func relay(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	copyDir := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if tcp, ok := dst.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}

	go copyDir(a, b)
	go copyDir(b, a)
	wg.Wait()
}
