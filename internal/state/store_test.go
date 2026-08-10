package state

import "testing"

func TestEnsureAuthGeneratesCreds(t *testing.T) {
	s := NewStore(t.TempDir())
	u, p, err := s.EnsureAuth()
	if err != nil {
		t.Fatal(err)
	}
	if len(u) != 12 || len(p) != 12 {
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
	if auth.Username != u || auth.Password != p {
		t.Fatalf("persisted creds mismatch")
	}
	if len(auth.SecretPath) != 24 {
		t.Fatalf("secret path length: %d", len(auth.SecretPath))
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
