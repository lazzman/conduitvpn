package purity

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLookupResidential(t *testing.T) {
	body := loadFixture(t, "residential.json")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/36.57.106.193" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if !strings.Contains(r.URL.RawQuery, "fields=") {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer ts.Close()

	c := NewClient(time.Second)
	c.baseURL = ts.URL
	c.minInterval = 0
	rec, err := c.Lookup(context.Background(), "36.57.106.193")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Source != "isp" || rec.Hosting || rec.Country != "CN" {
		t.Fatalf("lookup = %+v", rec)
	}
}

func TestLookupRateLimited(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Rl", "0")
		w.Header().Set("X-Ttl", "12")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "slow down")
	}))
	defer ts.Close()

	c := NewClient(time.Second)
	c.baseURL = ts.URL
	c.minInterval = 0
	_, err := c.Lookup(context.Background(), "8.8.8.8")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter != 12*time.Second {
		t.Fatalf("retry after = %v", err)
	}
	if RetryAfter(err) != 12*time.Second {
		t.Fatalf("RetryAfter = %s", RetryAfter(err))
	}
}

func TestLookupRejectsPrivateIP(t *testing.T) {
	c := NewClient(time.Second)
	if _, err := c.Lookup(context.Background(), "10.0.0.1"); err == nil {
		t.Fatal("expected invalid ip")
	}
}

func TestLookupUnexpectedStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer ts.Close()
	c := NewClient(time.Second)
	c.baseURL = ts.URL
	c.minInterval = 0
	_, err := c.Lookup(context.Background(), "8.8.8.8")
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v", err)
	}
}

func TestLookupThrottlesBetweenCalls(t *testing.T) {
	hits := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(loadFixture(t, "residential.json"))
	}))
	defer ts.Close()
	c := NewClient(time.Second)
	c.baseURL = ts.URL
	c.minInterval = 40 * time.Millisecond
	start := time.Now()
	if _, err := c.Lookup(context.Background(), "8.8.8.8"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Lookup(context.Background(), "1.1.1.1"); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Fatalf("elapsed %s, want >= 40ms", elapsed)
	}
	if hits != 2 {
		t.Fatalf("hits=%d", hits)
	}
}
