package purity

import (
	"os"
	"path/filepath"
	"testing"
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
	if rec.Source != "isp" || rec.Hosting {
		t.Fatalf("vpngate residential should not be hosting: %+v", rec)
	}
	if !hasAttr(rec, "vpn") || !hasAttr(rec, "anonymous") {
		t.Fatalf("attrs = %v", rec.Attrs)
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse([]byte(`{"data":{}}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestRecordViewError(t *testing.T) {
	info := Record{Error: "timeout", Source: "isp"}.View()
	if info.Status != StatusError || info.Source != "isp" {
		t.Fatalf("view = %+v", info)
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
