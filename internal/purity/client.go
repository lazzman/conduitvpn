package purity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultBaseURL = "http://ip-api.com"
	apiFields      = "status,message,country,countryCode,regionName,city,zip,isp,org,as,asname,mobile,proxy,hosting,query"
	// MinInterval keeps free-tier lookups under 45 requests/minute.
	MinInterval = 1500 * time.Millisecond
	maxBackoff  = 2 * time.Minute
)

// ErrRateLimited is returned when ip-api responds 429.
var ErrRateLimited = errors.New("ip-api rate limited")

// RateLimitError carries how long to wait after a 429.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	if e != nil && e.RetryAfter > 0 {
		return fmt.Sprintf("ip-api rate limited, retry after %s", e.RetryAfter)
	}
	return ErrRateLimited.Error()
}

func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// RetryAfter returns a bounded wait taken from a rate-limit error.
func RetryAfter(err error) time.Duration {
	var rl *RateLimitError
	if errors.As(err, &rl) && rl != nil && rl.RetryAfter > 0 {
		if rl.RetryAfter > maxBackoff {
			return maxBackoff
		}
		return rl.RetryAfter
	}
	return time.Minute
}

// Client looks up public IPv4 addresses through the ip-api.com JSON endpoint.
type Client struct {
	http        *http.Client
	baseURL     string
	minInterval time.Duration
	mu          sync.Mutex
	nextAllowed time.Time
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
		baseURL:     defaultBaseURL,
		minInterval: MinInterval,
	}
}

// Lookup fetches and parses the ip-api payload for ip.
func (c *Client) Lookup(ctx context.Context, ip string) (Record, error) {
	if !publicIPv4(ip) {
		return Record{}, fmt.Errorf("invalid ip %q", ip)
	}
	if err := c.acquire(ctx); err != nil {
		return Record{}, err
	}
	base := c.baseURL
	if base == "" {
		base = defaultBaseURL
	}
	u := strings.TrimRight(base, "/") + "/json/" + ip + "?fields=" + apiFields
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Record{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "conduitvpn/0.1")

	resp, err := c.http.Do(req)
	if err != nil {
		return Record{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Record{}, err
	}
	c.noteHeaders(resp.Header, resp.StatusCode)
	if resp.StatusCode == http.StatusTooManyRequests {
		return Record{}, &RateLimitError{RetryAfter: headerDuration(resp.Header, "X-Ttl")}
	}
	if resp.StatusCode != http.StatusOK {
		return Record{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return Parse(body)
}

func (c *Client) acquire(ctx context.Context) error {
	for {
		c.mu.Lock()
		wait := time.Until(c.nextAllowed)
		if wait <= 0 {
			interval := c.minInterval
			if interval < 0 {
				interval = 0
			}
			c.nextAllowed = time.Now().Add(interval)
			c.mu.Unlock()
			return nil
		}
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (c *Client) noteHeaders(h http.Header, status int) {
	remaining, hasRemaining := headerInt(h, "X-Rl")
	ttl := headerDuration(h, "X-Ttl")
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	next := now.Add(c.minInterval)
	if status == http.StatusTooManyRequests || (hasRemaining && remaining <= 0) {
		wait := ttl
		if wait <= 0 {
			wait = time.Minute
		}
		next = now.Add(wait)
	}
	if next.After(c.nextAllowed) {
		c.nextAllowed = next
	}
}

func headerInt(h http.Header, name string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(h.Get(name)))
	if err != nil {
		return 0, false
	}
	return n, true
}

func headerDuration(h http.Header, name string) time.Duration {
	n, ok := headerInt(h, name)
	if !ok || n < 0 {
		return 0
	}
	return time.Duration(n) * time.Second
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
