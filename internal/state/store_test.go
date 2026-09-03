package state

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestVPNGateSourcesRoundTripAndPermissions(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	sources := []string{"http://one.example", "https://source-password@two.example:8443"}
	if err := s.SaveVPNGateSources(VPNGateSources{Mirrors: sources}); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadVPNGateSources()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Mirrors) != len(sources) || got.Mirrors[0] != sources[0] || got.Mirrors[1] != sources[1] {
		t.Fatalf("sources = %#v, want %#v", got.Mirrors, sources)
	}
	info, err := os.Stat(s.VPNGateSourcesPath())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("sources mode = %o, want 600", got)
	}

	// A fresh Store instance reads the same persisted configuration.
	restarted, err := NewStore(dir).LoadVPNGateSources()
	if err != nil || len(restarted.Mirrors) != 2 {
		t.Fatalf("restarted sources = %#v, err=%v", restarted, err)
	}
}

func TestVPNGateSourcesEmptyListIsStableJSONArray(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.SaveVPNGateSources(VPNGateSources{}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(s.VPNGateSourcesPath())
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["mirrors"]) != "[]" {
		t.Fatalf("persisted mirrors = %s, want []", raw["mirrors"])
	}
	got, err := s.LoadVPNGateSources()
	if err != nil || got.Mirrors == nil {
		t.Fatalf("loaded empty mirrors = %#v, err=%v; want non-nil empty slice", got.Mirrors, err)
	}
}

func TestEnsureAuthGeneratesCreds(t *testing.T) {
	s := NewStore(t.TempDir())
	u, p, err := s.EnsureAuth()
	if err != nil {
		t.Fatal(err)
	}
	if len(u) != 12 || len(p) != 16 {
		t.Fatalf("cred length: user=%d pass=%d", len(u), len(p))
	}
	// second call: stable, no regeneration
	u2, p2, err := s.EnsureAuth()
	if err != nil {
		t.Fatal(err)
	}
	if u2 != "" || p2 != "" {
		t.Fatalf("should not regenerate: %q %q", u2, p2)
	}
	auth, _ := s.LoadAuth()
	if auth.Username != u || auth.Password != "" || !auth.VerifyPassword(p) {
		t.Fatalf("persisted creds mismatch")
	}
	if len(auth.SecretPath) != 24 {
		t.Fatalf("secret path length: %d", len(auth.SecretPath))
	}
}

func TestEnsureAuthWithDefaults(t *testing.T) {
	s := NewStore(t.TempDir())
	u, p, err := s.EnsureAuthWithDefaults("admin", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if u != "admin" || p != "demo" {
		t.Fatalf("generated credentials = %q/%q", u, p)
	}
	_, _, err = s.EnsureAuthWithDefaults("other", "other")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := s.LoadAuth()
	if err != nil {
		t.Fatal(err)
	}
	if auth.Username != "admin" || auth.Password != "" || !auth.VerifyPassword("demo") {
		t.Fatalf("existing credentials were overwritten: %+v", auth)
	}
}

func TestLegacyAuthMigratesWithoutChangingPassword(t *testing.T) {
	s := NewStore(t.TempDir())
	legacy := UIAuth{SecretPath: "0123456789abcdef01234567", Username: "admin", Password: "correct horse battery staple"}
	if err := writeJSON(s.AuthPath(), legacy); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureAuthConfigured("", "", false); err != nil {
		t.Fatal(err)
	}
	auth, err := s.LoadAuth()
	if err != nil {
		t.Fatal(err)
	}
	if auth.Password != "" || !auth.VerifyPassword("correct horse battery staple") {
		t.Fatalf("legacy credential was not migrated: %+v", auth)
	}
}

func TestPBKDF2SHA256RFCVector(t *testing.T) {
	got := hex.EncodeToString(pbkdf2SHA256([]byte("password"), []byte("salt"), 1, 32))
	const want = "120fb6cffcf8b32c43e7225256c4f837a86548c92ccc35480805987cb70be17b"
	if got != want {
		t.Fatalf("PBKDF2 result = %s, want %s", got, want)
	}
}

func TestStateFilesArePrivate(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.SaveRoute(Route{Mode: "auto"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.RoutePath())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("route mode = %o, want 600", got)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("data dir mode = %o, want 700", got)
	}
}

func TestSecureDirRepairsExistingPermissions(t *testing.T) {
	dir := t.TempDir()
	nested := dir + "/nested"
	if err := os.Mkdir(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	path := nested + "/old.json"
	if err := os.WriteFile(path, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SecureDir(dir); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file permission = %v, err = %v", info.Mode(), err)
	}
	info, err = os.Stat(nested)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory permission = %v, err = %v", info.Mode(), err)
	}
}

func TestEnsureStartupTemplate(t *testing.T) {
	dir := t.TempDir()
	if err := SecureDir(dir); err != nil {
		t.Fatal(err)
	}
	created, err := EnsureStartupTemplate(dir)
	if err != nil || !created {
		t.Fatalf("template created=%v err=%v", created, err)
	}
	path := dir + "/" + startupTemplateName
	contents, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(contents), "NETWORK_MODE=host") {
		t.Fatalf("template contents=%q err=%v", contents, err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("template permissions=%v err=%v", info.Mode(), err)
	}
	created, err = EnsureStartupTemplate(dir)
	if err != nil || created {
		t.Fatalf("existing template created=%v err=%v", created, err)
	}
}

func TestRandomCredShape(t *testing.T) {
	for i := 0; i < 50; i++ {
		c := randomCred(12)
		hasLower, hasUpper, hasDigit := false, false, false
		for _, r := range c {
			if r >= 'a' && r <= 'z' {
				hasLower = true
			}
			if r >= 'A' && r <= 'Z' {
				hasUpper = true
			}
			if r >= '0' && r <= '9' {
				hasDigit = true
			}
		}
		if !hasLower || !hasUpper || !hasDigit {
			t.Fatalf("bad cred %q", c)
		}
	}
}
