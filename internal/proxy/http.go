package proxy

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/base64"
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
	const basicPrefix = "Basic "
	value := req.Header.Get("Proxy-Authorization")
	if !strings.HasPrefix(value, basicPrefix) {
		return false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(value, basicPrefix)))
	if err != nil {
		return false
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	if !ok {
		return false
	}
	return constantTimeEq(user, s.user) && constantTimeEq(pass, s.pass)
}

func constantTimeEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// dial opens an outbound connection through the configured egress policy.
// DNS uses a fixed public resolver through that same policy.
func (s *Server) dial(target string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if s.egress == nil {
		return nil, fmt.Errorf("proxy egress is not configured")
	}
	// Resolve hostnames through the same constrained egress path as the TCP
	// connection, so host mode cannot leak DNS through the host resolver.
	return s.dialResolved(ctx, normalizeTarget(target), tunnelResolver(s.egress, s.dns))
}

func (s *Server) dialResolved(ctx context.Context, target string, resolver *net.Resolver) (net.Conn, error) {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	if ip := net.ParseIP(host); ip != nil {
		return s.egress.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port), 20*time.Second, 30*time.Second)
	}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, addr := range addrs {
		if ip := addr.IP.To4(); ip != nil {
			return s.egress.DialContext(ctx, "tcp", net.JoinHostPort(ip.String(), port), 20*time.Second, 30*time.Second)
		}
	}
	return nil, fmt.Errorf("no IPv4 address for %s", host)
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
