// Package health implements the tunnel liveness probe: a TCP+TLS+HTTP
// request to a hardcoded IP, so the check itself never depends on DNS.
// InsecureSkipVerify is intentional — this is a reachability probe, not
// an authentication decision.
package health

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"conduitvpn/internal/egress"
)

type Prober struct {
	Addr    string // "1.1.1.1:443"
	URL     string
	Timeout time.Duration
	client  *http.Client
}

func NewProber(addr string, timeout time.Duration, egressCtl *egress.Controller) *Prober {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return egressCtl.DialContext(ctx, network, address, timeout, 30*time.Second)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &Prober{
		Addr:    addr,
		URL:     "https://" + addr + "/generate_204",
		Timeout: timeout,
		client:  &http.Client{Transport: transport, Timeout: timeout},
	}
}

// Check returns nil when the tunnel egress answers HTTP 204.
func (p *Prober) Check(ctx context.Context) error {
	reqCtx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, p.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "conduitvpn/0.1 health")

	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected probe status %d", resp.StatusCode)
	}
	return nil
}
