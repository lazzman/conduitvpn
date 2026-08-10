// Package state persists app data as JSON files with atomic writes
// (write-temp-then-rename) so a crash never leaves a half-written file.
package state

import (
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"conduitvpn/internal/node"
)

type Store struct {
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

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

// UIAuth holds the web UI credentials and secret path, mirroring the
// original Python version: random username/password generated on first
// run and persisted in plaintext so users can edit it directly.
type UIAuth struct {
	SecretPath string `json:"secret_path"`
	Username   string `json:"username"`
	Password   string `json:"password"`
}

func (s *Store) AuthPath() string { return filepath.Join(s.dir, "ui_auth.json") }

// EnsureAuth loads (or creates on first run) the UI auth config. When
// freshly created it returns the generated username/password so the
// startup log can surface them.
func (s *Store) EnsureAuth() (genUser, genPass string, err error) {
	return s.ensureAuth("", "")
}

// EnsureAuthWithDefaults initializes absent credentials with the supplied
// defaults. Existing credentials are retained, including a previously
// generated secret path.
func (s *Store) EnsureAuthWithDefaults(username, password string) (genUser, genPass string, err error) {
	return s.ensureAuth(username, password)
}

func (s *Store) ensureAuth(defaultUser, defaultPass string) (genUser, genPass string, err error) {
	auth, err := s.LoadAuth()
	fresh := false
	if auth.SecretPath == "" {
		auth.SecretPath = randHex(24)
		fresh = true
	}
	if auth.Username == "" {
		if defaultUser != "" {
			auth.Username = defaultUser
		} else {
			auth.Username = randomCred(12)
		}
		fresh = true
	}
	if auth.Password == "" {
		if defaultPass != "" {
			auth.Password = defaultPass
		} else {
			auth.Password = randomCred(12)
		}
		fresh = true
	}
	if err := writeJSON(s.AuthPath(), auth); err != nil {
		return "", "", err
	}
	if fresh {
		return auth.Username, auth.Password, nil
	}
	return "", "", nil
}

func (s *Store) LoadAuth() (UIAuth, error) {
	var auth UIAuth
	if err := readJSON(s.AuthPath(), &auth); err != nil {
		return auth, err
	}
	return auth, nil
}

func (s *Store) SaveAuth(auth UIAuth) error {
	return writeJSON(s.AuthPath(), auth)
}

// SecretPath returns the persisted secret path, generating and saving a
// fresh one on first run.
func (s *Store) SecretPath() string {
	var auth UIAuth
	if err := readJSON(s.AuthPath(), &auth); err == nil && auth.SecretPath != "" {
		return auth.SecretPath
	}
	auth.SecretPath = randHex(24)
	_ = writeJSON(s.AuthPath(), auth)
	return auth.SecretPath
}

func randHex(n int) string {
	const hexChars = "0123456789abcdef"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// extremely unlikely; fall back to time-based entropy
		for i := range b {
			b[i] = byte(time.Now().UnixNano() >> uint(i%8))
		}
	}
	for i := range b {
		b[i] = hexChars[b[i]&0x0f]
	}
	return string(b)
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
			for i := range b {
				b[i] = byte(time.Now().UnixNano() >> uint((i+7)%32))
			}
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
