package vpngate

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestClientRejectsHTTPRedirects(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("redirect target"))
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := NewClient(nil, time.Second)
	_, err := client.Fetch(context.Background(), server.URL)
	if err == nil || !strings.Contains(err.Error(), "unexpected status 307") {
		t.Fatalf("redirect fetch error = %v, want status 307", err)
	}
}

func TestClientDoesNotRequestGzipCompression(t *testing.T) {
	var acceptEncoding string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		acceptEncoding = r.Header.Get("Accept-Encoding")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("*vpn_servers\n"))
	}))
	defer server.Close()

	client := NewClient(nil, time.Second)
	if _, err := client.Fetch(context.Background(), server.URL); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if strings.Contains(acceptEncoding, "gzip") {
		t.Fatalf("Accept-Encoding = %q, must not request gzip from legacy mirrors", acceptEncoding)
	}
}

func TestClientUsesURLUserInfoForBasicAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if !ok || user != "source-password" || password != "" {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("*vpn_servers\n"))
	}))
	defer server.Close()

	authenticatedURL := strings.Replace(server.URL, "://", "://source-password@", 1)
	client := NewClient(nil, time.Second)
	if _, err := client.Fetch(context.Background(), authenticatedURL); err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
}

func TestClientFetchErrorDoesNotExposeURLUserInfo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	authenticatedURL := strings.Replace(server.URL, "://", "://source-password@", 1)
	server.Close()

	client := NewClient(nil, time.Second)
	_, err := client.Fetch(context.Background(), authenticatedURL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want connection failure")
	}
	if strings.Contains(err.Error(), "source-password") {
		t.Fatalf("Fetch() error leaked URL userinfo: %v", err)
	}
}

func TestClientInvalidURLDoesNotExposeURLUserInfo(t *testing.T) {
	client := NewClient(nil, time.Second)
	_, err := client.Fetch(context.Background(), "https://source-password@%zz")
	if err == nil {
		t.Fatal("Fetch() error = nil, want URL parse failure")
	}
	if strings.Contains(err.Error(), "source-password") {
		t.Fatalf("Fetch() error leaked URL userinfo: %v", err)
	}
	if err.Error() != "invalid VPNGate API URL" {
		t.Fatalf("Fetch() error = %q", err)
	}
}

// fakeSocks5Server implements just enough SOCKS5 to validate the client's
// wire behavior: greeting → (auth) → CONNECT request → reply.
type fakeSocks5Server struct {
	ln          net.Listener
	mu          sync.Mutex
	targets     []string // recorded CONNECT targets
	requireAuth bool
}

func startFakeSocks5(t *testing.T, requireAuth bool) *fakeSocks5Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &fakeSocks5Server{ln: ln, requireAuth: requireAuth}
	go s.serve()
	return s
}

func (s *fakeSocks5Server) addr() string { return s.ln.Addr().String() }
func (s *fakeSocks5Server) close()       { s.ln.Close() }

func (s *fakeSocks5Server) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(conn)
	}
}

func (s *fakeSocks5Server) handle(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 256)
	// greeting: VER NMETHODS METHODS...
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	nmethods := int(buf[1])
	if _, err := io.ReadFull(conn, buf[:nmethods]); err != nil {
		return
	}
	if s.requireAuth {
		if _, err := conn.Write([]byte{0x05, 0x02}); err != nil {
			return
		}
		// RFC1929 auth: VER ULEN UNAME PLEN PASSWD
		if _, err := io.ReadFull(conn, buf[:2]); err != nil {
			return
		}
		ulen := int(buf[1])
		if _, err := io.ReadFull(conn, buf[:ulen]); err != nil {
			return
		}
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		plen := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:plen]); err != nil {
			return
		}
		if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
			return
		}
	} else {
		if _, err := conn.Write([]byte{0x05, 0x00}); err != nil {
			return
		}
	}
	// CONNECT request: VER CMD RSV ATYP
	if _, err := io.ReadFull(conn, buf[:4]); err != nil {
		return
	}
	atyp := buf[3]
	switch atyp {
	case 0x01:
		if _, err := io.ReadFull(conn, buf[:4]); err != nil {
			return
		}
		s.mu.Lock()
		s.targets = append(s.targets, net.IP(buf[:4]).String())
		s.mu.Unlock()
	case 0x03:
		if _, err := io.ReadFull(conn, buf[:1]); err != nil {
			return
		}
		l := int(buf[0])
		if _, err := io.ReadFull(conn, buf[:l]); err != nil {
			return
		}
		s.mu.Lock()
		s.targets = append(s.targets, string(buf[:l]))
		s.mu.Unlock()
	}
	if _, err := io.ReadFull(conn, buf[:2]); err != nil {
		return
	}
	// reply: success, IPv4 0.0.0.0:0
	_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0, 0, 0, 0, 0, 0})
	// keep the conn open briefly; client closes after handshake
	time.Sleep(50 * time.Millisecond)
}

func TestSocks5Connect(t *testing.T) {
	s := startFakeSocks5(t, false)
	defer s.close()

	d := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := socks5Connect(context.Background(), s.addr(), "example.com:443", "", "", d)
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	conn.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.targets) != 1 || s.targets[0] != "example.com" {
		t.Fatalf("targets = %v, want [example.com]", s.targets)
	}
}

func TestSocks5ConnectWithAuth(t *testing.T) {
	s := startFakeSocks5(t, true)
	defer s.close()

	d := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := socks5Connect(context.Background(), s.addr(), "8.8.8.8:53", "user", "pass", d)
	if err != nil {
		t.Fatalf("authed connect failed: %v", err)
	}
	conn.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.targets) != 1 || s.targets[0] != "8.8.8.8" {
		t.Fatalf("targets = %v, want [8.8.8.8]", s.targets)
	}
}

func TestSocks5ConnectAuthRejected(t *testing.T) {
	// server requires auth but client has no credentials
	s := startFakeSocks5(t, true)
	defer s.close()

	d := &net.Dialer{Timeout: 3 * time.Second}
	_, err := socks5Connect(context.Background(), s.addr(), "example.com:443", "", "", d)
	if err == nil {
		t.Fatal("expected auth failure")
	}
}
