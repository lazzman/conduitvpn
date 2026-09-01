package purity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const defaultBaseURL = "https://ipinfo.io/widget/demo"

// ErrRateLimited is returned when the widget API responds 429.
var ErrRateLimited = errors.New("ipinfo rate limited")

// Client looks up public IPv4 addresses through the ipinfo widget demo API.
type Client struct {
	http    *http.Client
	baseURL string
}

// NewClient builds a standard-library HTTP client. timeout covers the whole request.
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	return &Client{
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext:           (&net.Dialer{Timeout: timeout}).DialContext,
				TLSHandshakeTimeout:   timeout,
				ResponseHeaderTimeout: timeout,
			},
		},
		baseURL: defaultBaseURL,
	}
}

// Lookup fetches and parses the widget payload for ip.
func (c *Client) Lookup(ctx context.Context, ip string) (Record, error) {
	if !publicIPv4(ip) {
		return Record{}, fmt.Errorf("invalid ip %q", ip)
	}
	base := c.baseURL
	if base == "" {
		base = defaultBaseURL
	}
	u := strings.TrimRight(base, "/") + "/" + ip
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Record{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "conduitvpn/0.1")
	req.Header.Set("Referer", "https://ipinfo.io/")

	resp, err := c.http.Do(req)
	if err != nil {
		return Record{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Record{}, err
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return Record{}, ErrRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return Record{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return Parse(body)
}

func publicIPv4(s string) bool {
	ip := net.ParseIP(s)
	if ip == nil {
		return false
	}
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	return v4.IsGlobalUnicast() && !v4.IsPrivate()
}
