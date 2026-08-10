package proxy

import (
	"bufio"
	"context"
	"crypto/subtle"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"conduitvpn/internal/logx"
)

// handleHTTP serves an HTTP proxy request: CONNECT tunneling or
// absolute-URI forwarding. One upstream connection per request; the
// response is streamed back verbatim.
func (s *Server) handleHTTP(client net.Conn, br *bufio.Reader) {
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	if s.authOn && !s.checkHTTPAuth(req) {
		_, _ = client.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n" +
			"Proxy-Authenticate: Basic realm=\"conduitvpn\"\r\nContent-Length: 0\r\n\r\n"))
		return
	}
	_ = client.SetReadDeadline(time.Time{})

	target := req.URL.Host
	if target == "" {
		_, _ = client.Write([]byte("HTTP/1.1 400 Bad Request\r\nContent-Length: 0\r\n\r\n"))
		return
	}

	if req.Method == http.MethodConnect {
		upstream, err := s.dial(target)
		if err != nil {
			logx.Debug("connect dial failed", "target", target, "err", err)
			_, _ = client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
			return
		}
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		relay(client, upstream)
		return
	}

	upstream, err := s.dial(target)
	if err != nil {
		logx.Debug("http dial failed", "target", target, "err", err)
		_, _ = client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
		return
	}
	defer upstream.Close()

	// Rewrite to origin-form and disable keep-alive (one request per
	// upstream connection keeps the relay logic simple).
	req.RequestURI = ""
	req.Close = true
	if req.Host == "" {
		req.Host = req.URL.Host
	}
	if err := req.Write(upstream); err != nil {
		return
	}
	_, _ = io.Copy(client, upstream)
}

func (s *Server) checkHTTPAuth(req *http.Request) bool {
	user, pass, ok := req.BasicAuth()
	if !ok {
		return false
	}
	return constantTimeEq(user, s.user) && constantTimeEq(pass, s.pass)
}

func constantTimeEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// dial opens an outbound connection to host:port through the tunnel.
// DNS runs through a fixed public resolver so resolution egresses via
// the VPN default route rather than the container's resolv.conf chain.
func (s *Server) dial(target string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	d := net.Dialer{
		Timeout:   20 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver:  tunnelResolver(s.dns),
	}
	return d.DialContext(ctx, "tcp", normalizeTarget(target))
}

// normalizeTarget fills in the default port and strips any scheme
// prefix a client may have included.
func normalizeTarget(target string) string {
	t := target
	if i := strings.Index(t, "://"); i >= 0 {
		t = t[i+3:]
	}
	if _, _, err := net.SplitHostPort(t); err != nil {
		t = net.JoinHostPort(t, "80")
	}
	return t
}

var _ = fmt.Sprintf // keep fmt if unused in future edits
