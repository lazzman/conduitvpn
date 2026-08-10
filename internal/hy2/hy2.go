// Package hy2 runs a hysteria2 inbound gateway on the proxy path: hy2
// clients connect to the container and their traffic egresses through
// the 方案 B tunnel (sing-box "direct" outbound → default route → tun0).
package hy2

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"conduitvpn/internal/logx"
	"conduitvpn/internal/upstream"
)

type Config struct {
	Port     int
	Bind     string
	Password string
	ObfsPass string // optional salamander obfs
}

// Start writes the sing-box hysteria2 inbound config and runs it.
func Start(ctx context.Context, dataDir string, cfg Config) (*upstream.Runner, error) {
	if cfg.Password == "" {
		return nil, fmt.Errorf("HY2_PASSWORD is required when hy2 is enabled")
	}
	if cfg.Bind == "" {
		cfg.Bind = "0.0.0.0"
	}

	dir := filepath.Join(dataDir, "hy2")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	certPath := filepath.Join(dir, "cert.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if err := generateCert(certPath, keyPath); err != nil {
		return nil, err
	}

	ib := map[string]any{
		"type":        "hysteria2",
		"tag":         "hy2-in",
		"listen":      cfg.Bind,
		"listen_port": cfg.Port,
		"users":       []any{map[string]any{"password": cfg.Password}},
		"tls": map[string]any{
			"enabled":          true,
			"certificate_path": certPath,
			"key_path":         keyPath,
		},
	}
	if cfg.ObfsPass != "" {
		ib["obfs"] = map[string]any{"type": "salamander", "password": cfg.ObfsPass}
	}

	full := map[string]any{
		"log":      map[string]any{"level": "warn"},
		"inbounds": []any{ib},
		"outbounds": []any{
			map[string]any{"type": "direct", "tag": "direct"},
		},
	}

	cfgPath := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(full, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(cfgPath, data, 0o600); err != nil {
		return nil, err
	}

	runner, err := upstream.StartRunnerUDP(ctx, cfgPath, cfg.Port)
	if err != nil {
		return nil, err
	}
	logx.Info("hy2 inbound listening", "port", cfg.Port, "udp", true, "obfs", cfg.ObfsPass != "")
	return runner, nil
}

// generateCert creates a self-signed ECDSA certificate for the hy2 TLS.
func generateCert(certPath, keyPath string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().Unix()),
		Subject:               pkix.Name{CommonName: "conduitvpn"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}

	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
		return err
	}

	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	keyDer, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return err
	}
	return pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer})
}
