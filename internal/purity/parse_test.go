package purity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseResidential(t *testing.T) {
	rec, err := Parse(loadFixture(t, "residential.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Source != "isp" || rec.Hosting || rec.Country != "CN" || rec.Postal != "230000" || rec.City != "Hefei" {
		t.Fatalf("residential record = %+v", rec)
	}
	if rec.Org != "Chinanet AH" || rec.Region != "Anhui" {
		t.Fatalf("org/region = %+v", rec)
	}
	if len(rec.Attrs) != 0 {
		t.Fatalf("attrs = %v", rec.Attrs)
	}
}

func TestParseHosting(t *testing.T) {
	rec, err := Parse(loadFixture(t, "hosting.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Source != "hosting" || !rec.Hosting || rec.Country != "US" || rec.Postal != "95119" {
		t.Fatalf("hosting record = %+v", rec)
	}
	if !hasAttr(rec, "hosting") {
		t.Fatalf("missing hosting attr: %v", rec.Attrs)
	}
}

func TestParseVPNGateResidential(t *testing.T) {
	rec, err := Parse(loadFixture(t, "vpngate_isp.json"))
	if err != nil {
		t.Fatal(err)
	}
	if rec.Source != "isp" || rec.Hosting || rec.Country != "KR" || rec.Postal != "03121" {
		t.Fatalf("vpngate residential should not be hosting: %+v", rec)
	}
}

func TestParseMobileProxy(t *testing.T) {
	body := []byte(`{"status":"success","countryCode":"JP","city":"Tokyo","zip":"100-0001","isp":"NTT","mobile":true,"proxy":true,"hosting":false,"query":"203.0.113.9"}`)
	rec, err := Parse(body)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Source != "isp" || rec.Hosting || rec.Country != "JP" {
		t.Fatalf("record = %+v", rec)
	}
	if !hasAttr(rec, "mobile") || !hasAttr(rec, "proxy") {
		t.Fatalf("attrs = %v", rec.Attrs)
	}
}

func TestParseFailStatus(t *testing.T) {
	_, err := Parse([]byte(`{"status":"fail","message":"invalid query","query":"10.0.0.1"}`))
	if err == nil || !strings.Contains(err.Error(), "invalid query") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse([]byte(`{"status":"success"}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestRecordViewError(t *testing.T) {
	info := Record{Error: "timeout", Source: "isp"}.View()
	if info.Status != StatusError || info.Source != "isp" {
		t.Fatalf("view = %+v", info)
	}
}

func TestRecordFreshTTL(t *testing.T) {
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	fresh := Record{Source: "isp", CheckedAt: now.Add(-time.Hour).Format(time.RFC3339)}
	stale := Record{Source: "isp", CheckedAt: now.Add(-25 * time.Hour).Format(time.RFC3339)}
	errFresh := Record{Error: "unexpected status 502", CheckedAt: now.Add(-5 * time.Minute).Format(time.RFC3339)}
	errStale := Record{Error: "unexpected status 502", CheckedAt: now.Add(-31 * time.Minute).Format(time.RFC3339)}
	if !fresh.Fresh(now) || stale.Fresh(now) {
		t.Fatalf("success ttl: fresh=%v stale=%v", fresh.Fresh(now), stale.Fresh(now))
	}
	if !errFresh.Fresh(now) || errStale.Fresh(now) {
		t.Fatalf("error ttl: fresh=%v stale=%v", errFresh.Fresh(now), errStale.Fresh(now))
	}
	if (Record{CheckedAt: "demo"}).Fresh(now) {
		t.Fatal("non-RFC3339 timestamp should be stale")
	}
}

func hasAttr(rec Record, name string) bool {
	for _, a := range rec.Attrs {
		if a == name {
			return true
		}
	}
	return false
}
