package vpngate

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"conduitvpn/internal/config"
)

// Client fetches the VPNGate node list, optionally through an upstream
// proxy (HTTP CONNECT or SOCKS5) when the direct path is blocked.
type Client struct {
	http *http.Client
}

// NewClient builds an API client. When upstream is set, all traffic
// egresses through it.
func NewClient(upstream *config.UpstreamProxy, timeout time.Duration) *Client {
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		// VPNGate mirrors are backed by a few legacy IIS deployments that
		// advertise gzip but send an invalid chunked response. Keep the
		// payload uncompressed so the client can reliably consume the CSV.
		DisableCompression: true,
	}
	if upstream != nil {
		switch upstream.Type {
		case "http":
			transport.Proxy = http.ProxyURL(&url.URL{Scheme: "http", Host: upstream.Addr})
			if upstream.User != "" {
				auth := "Basic " + base64.StdEncoding.EncodeToString(
					[]byte(upstream.User+":"+upstream.Pass))
				transport.ProxyConnectHeader = http.Header{"Proxy-Authorization": []string{auth}}
			}
		case "socks5":
			d := &net.Dialer{Timeout: timeout}
			transport.DialContext = func(ctx context.Context, _, addr string) (net.Conn, error) {
				return socks5Connect(ctx, upstream.Addr, addr, upstream.User, upstream.Pass, d)
			}
			transport.TLSClientConfig = &tls.Config{}
		}
	}
	return &Client{http: &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Fetch downloads the VPNGate server list.
func (c *Client) Fetch(ctx context.Context, apiURL string) ([]byte, error) {
	reqCtx, cancel := context.WithTimeout(ctx, c.http.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// socks5Connect performs a SOCKS5 (RFC 1928 + RFC 1929 auth) connect
// through the proxy to target.
func socks5Connect(ctx context.Context, proxyAddr, target, user, pass string, d *net.Dialer) (net.Conn, error) {
	conn, err := d.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("socks5 dial %s: %w", proxyAddr, err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = conn.Close()
		}
	}()

	// greeting: offer no-auth, plus user/pass if credentials configured
	methods := []byte{0x00}
	if user != "" {
		methods = append(methods, 0x02)
	}
	if _, err := conn.Write(append([]byte{0x05, byte(len(methods))}, methods...)); err != nil {
		return nil, err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, err
	}
	if resp[0] != 0x05 {
		return nil, errors.New("socks5: bad version from proxy")
	}
	switch resp[1] {
	case 0x00: // no auth
	case 0x02:
		if user == "" {
			return nil, errors.New("socks5: proxy requires auth")
		}
		if err := socks5ClientAuth(conn, user, pass); err != nil {
			return nil, err
		}
	case 0xFF:
		return nil, errors.New("socks5: no acceptable auth method")
	default:
		return nil, fmt.Errorf("socks5: unexpected method %d", resp[1])
	}

	host, portStr, err := net.SplitHostPort(target)
	if err != nil {
		return nil, err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, err
	}

	req := []byte{0x05, 0x01, 0x00} // CONNECT
	if ip := net.ParseIP(host); ip != nil {
		if v4 := ip.To4(); v4 != nil {
			req = append(req, 0x01)
			req = append(req, v4...)
		} else {
			req = append(req, 0x04)
			req = append(req, ip.To16()...)
		}
	} else {
		if len(host) > 255 {
			return nil, errors.New("socks5: hostname too long")
		}
		req = append(req, 0x03, byte(len(host)))
		req = append(req, host...)
	}
	req = append(req, byte(port>>8), byte(port))
	if _, err := conn.Write(req); err != nil {
		return nil, err
	}

	head := make([]byte, 4)
	if _, err := io.ReadFull(conn, head); err != nil {
		return nil, err
	}
	if head[1] != 0x00 {
		return nil, fmt.Errorf("socks5: connect failed (code %d)", head[1])
	}
	switch head[3] {
	case 0x01:
		_, err = io.CopyN(io.Discard, conn, 4)
	case 0x04:
		_, err = io.CopyN(io.Discard, conn, 16)
	case 0x03:
		var l [1]byte
		if _, err = io.ReadFull(conn, l[:]); err == nil {
			_, err = io.CopyN(io.Discard, conn, int64(l[0]))
		}
	default:
		err = fmt.Errorf("socks5: bad reply address type %d", head[3])
	}
	if err != nil {
		return nil, err
	}
	var portBuf [2]byte
	if _, err := io.ReadFull(conn, portBuf[:]); err != nil {
		return nil, err
	}

	ok = true
	return conn, nil
}

func socks5ClientAuth(conn net.Conn, user, pass string) error {
	if len(user) > 255 || len(pass) > 255 {
		return errors.New("socks5: credentials too long")
	}
	req := []byte{0x01, byte(len(user))}
	req = append(req, user...)
	req = append(req, byte(len(pass)))
	req = append(req, pass...)
	if _, err := conn.Write(req); err != nil {
		return err
	}
	resp := make([]byte, 2)
	if _, err := io.ReadFull(conn, resp); err != nil {
		return err
	}
	if resp[1] != 0x00 {
		return errors.New("socks5: auth rejected")
	}
	return nil
}
