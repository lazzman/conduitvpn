// Package state persists app data as JSON files with atomic writes
// (write-temp-then-rename) so a crash never leaves a half-written file.
package state

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"unicode/utf8"

	"conduitvpn/internal/node"
)

type Store struct {
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

// SecureDir repairs permissions from older releases without following
// symlinks. State files contain credentials and OpenVPN private material.
func SecureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if entry.IsDir() {
			return os.Chmod(path, 0o700)
		}
		if entry.Type().IsRegular() {
			return os.Chmod(path, 0o600)
		}
		return nil
	})
}

func (s *Store) NodesPath() string { return filepath.Join(s.dir, "nodes.json") }

func (s *Store) SaveNodes(nodes []*node.Node) error {
	return writeJSON(s.NodesPath(), nodes)
}

func (s *Store) LoadNodes() ([]*node.Node, error) {
	var nodes []*node.Node
	if err := readJSON(s.NodesPath(), &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

// BlacklistEntry records why and when a node was blacklisted.
type BlacklistEntry struct {
	Reason   string `json:"reason"`
	MarkedAt string `json:"marked_at"`
}

func (s *Store) BlacklistPath() string { return filepath.Join(s.dir, "blacklist.json") }

func (s *Store) SaveBlacklist(bl map[string]BlacklistEntry) error {
	return writeJSON(s.BlacklistPath(), bl)
}

func (s *Store) LoadBlacklist() (map[string]BlacklistEntry, error) {
	bl := map[string]BlacklistEntry{}
	if err := readJSON(s.BlacklistPath(), &bl); err != nil {
		return nil, err
	}
	return bl, nil
}

// Route holds the routing mode configuration (auto / country / fixed).
type Route struct {
	Mode    string `json:"mode"`
	Country string `json:"country"`
	Node    string `json:"node"`
}

func (s *Store) RoutePath() string { return filepath.Join(s.dir, "route.json") }

func (s *Store) SaveRoute(r Route) error { return writeJSON(s.RoutePath(), r) }

func (s *Store) LoadRoute() (Route, error) {
	var r Route
	if err := readJSON(s.RoutePath(), &r); err != nil {
		return r, err
	}
	return r, nil
}

const passwordIterations = 600000

// UIAuth holds the web UI credentials and secret path. Password is retained
// only to migrate legacy files and is cleared when the file is next written.
type UIAuth struct {
	SecretPath         string `json:"secret_path"`
	Username           string `json:"username"`
	Password           string `json:"password,omitempty"`
	PasswordSalt       string `json:"password_salt,omitempty"`
	PasswordHash       string `json:"password_hash,omitempty"`
	PasswordIterations int    `json:"password_iterations,omitempty"`
}

func (s *Store) AuthPath() string { return filepath.Join(s.dir, "ui_auth.json") }

// EnsureAuth remains for demos and tests. Production startup must use
// EnsureAuthConfigured so credentials are never generated or logged.
func (s *Store) EnsureAuth() (genUser, genPass string, err error) {
	if _, err := s.LoadAuth(); err == nil {
		return "", "", s.EnsureAuthConfigured("", "", true)
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	user, pass := randomCred(12), randomCred(16)
	if err := s.EnsureAuthConfigured(user, pass, true); err != nil {
		return "", "", err
	}
	return user, pass, nil
}

// EnsureAuthWithDefaults initializes absent credentials with the supplied
// defaults. Existing credentials are retained, including a previously
// generated secret path.
func (s *Store) EnsureAuthWithDefaults(username, password string) (genUser, genPass string, err error) {
	if _, err := s.LoadAuth(); err == nil {
		return "", "", s.EnsureAuthConfigured("", "", true)
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	if err := s.EnsureAuthConfigured(username, password, true); err != nil {
		return "", "", err
	}
	return username, password, nil
}

// EnsureAuthConfigured creates credentials only from explicit configuration.
// Existing plaintext credentials are transparently migrated to a salted hash.
func (s *Store) EnsureAuthConfigured(username, password string, demo bool) error {
	auth, err := s.LoadAuth()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if (username == "") != (password == "") {
		return fmt.Errorf("UI_USER and UI_PASSWORD must be configured together")
	}
	if username != "" {
		if utf8.RuneCountInString(password) < 16 && !demo {
			return fmt.Errorf("UI_PASSWORD must contain at least 16 characters")
		}
		auth.Username = username
		if err := auth.setPassword(password); err != nil {
			return err
		}
	}
	if auth.SecretPath == "" {
		secret, err := randHex(24)
		if err != nil {
			return err
		}
		auth.SecretPath = secret
	}
	if auth.Password != "" {
		if err := auth.setPassword(auth.Password); err != nil {
			return err
		}
	}
	if auth.Username == "" || auth.PasswordHash == "" || auth.PasswordSalt == "" {
		return fmt.Errorf("web UI credentials are not initialized; set UI_USER and UI_PASSWORD")
	}
	return s.SaveAuth(auth)
}

func (s *Store) LoadAuth() (UIAuth, error) {
	var auth UIAuth
	if err := readJSON(s.AuthPath(), &auth); err != nil {
		return auth, err
	}
	return auth, nil
}

func (s *Store) SaveAuth(auth UIAuth) error {
	auth.Password = ""
	return writeJSON(s.AuthPath(), auth)
}

// VerifyPassword verifies a submitted password without retaining plaintext.
func (a UIAuth) VerifyPassword(password string) bool {
	if a.PasswordHash == "" || a.PasswordSalt == "" || a.PasswordIterations != passwordIterations {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(a.PasswordHash)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := pbkdf2SHA256([]byte(password), []byte(a.PasswordSalt), a.PasswordIterations, len(want))
	return hmac.Equal(got, want)
}

func (a *UIAuth) setPassword(password string) error {
	salt, err := randHex(16)
	if err != nil {
		return err
	}
	a.PasswordSalt = salt
	a.PasswordHash = base64.RawStdEncoding.EncodeToString(pbkdf2SHA256([]byte(password), []byte(salt), passwordIterations, sha256.Size))
	a.PasswordIterations = passwordIterations
	a.Password = ""
	return nil
}

// SecretPath returns the persisted secret path, generating and saving a
// fresh one on first run.
func (s *Store) SecretPath() string {
	var auth UIAuth
	if err := readJSON(s.AuthPath(), &auth); err == nil && auth.SecretPath != "" {
		return auth.SecretPath
	}
	secret, err := randHex(24)
	if err != nil {
		return ""
	}
	auth.SecretPath = secret
	_ = writeJSON(s.AuthPath(), auth)
	return auth.SecretPath
}

func randHex(n int) (string, error) {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = hexChars[b[i]&0x0f]
	}
	return string(b), nil
}

// randomCred generates a 12-char credential with lower+upper+digit.
func randomCred(n int) string {
	const lower = "abcdefghijklmnopqrstuvwxyz"
	const upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const digits = "0123456789"
	chars := []byte(lower + upper + digits)
	for {
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			panic(fmt.Sprintf("crypto/rand: %v", err))
		}
		for i := range b {
			b[i] = chars[int(b[i])%len(chars)]
		}
		if b[0] >= '0' && b[0] <= '9' {
			b[0] = lower[int(b[0])%26]
		}
		hasLower, hasUpper, hasDigit := false, false, false
		for _, c := range b {
			switch {
			case c >= 'a' && c <= 'z':
				hasLower = true
			case c >= 'A' && c <= 'Z':
				hasUpper = true
			case c >= '0' && c <= '9':
				hasDigit = true
			}
		}
		if hasLower && hasUpper && hasDigit {
			return string(b)
		}
	}
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".conduitvpn-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// pbkdf2SHA256 is the RFC 8018 PBKDF2 construction using only standard
// library primitives. Tests verify its output against RFC test vectors.
func pbkdf2SHA256(password, salt []byte, iterations, length int) []byte {
	if iterations < 1 || length < 1 {
		return nil
	}
	result := make([]byte, 0, length)
	for block := uint32(1); len(result) < length; block++ {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		_, _ = mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := append([]byte(nil), u...)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		result = append(result, t...)
	}
	return result[:length]
}
