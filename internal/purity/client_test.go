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
		if r.URL.Path != "/widget/demo/36.57.106.193" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer ts.Close()

	c := NewClient(time.Second)
	c.baseURL = ts.URL + "/widget/demo"
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
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, "slow down")
	}))
	defer ts.Close()

	c := NewClient(time.Second)
	c.baseURL = ts.URL
	_, err := c.Lookup(context.Background(), "8.8.8.8")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
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
	_, err := c.Lookup(context.Background(), "8.8.8.8")
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err = %v", err)
	}
}
