package hy2

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if err := generateCert(certPath, keyPath); err != nil {
		t.Fatalf("generateCert: %v", err)
	}
	for _, p := range []string{certPath, keyPath} {
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("missing %s: %v", p, err)
		}
		if st.Size() == 0 {
			t.Fatalf("%s is empty", p)
		}
	}
}

func TestStartRequiresPassword(t *testing.T) {
	_, err := Start(context.Background(), t.TempDir(), Config{Port: 7929, Bind: "127.0.0.1", Password: ""})
	if err == nil {
		t.Fatal("expected error when HY2_PASSWORD is empty")
	}
}
