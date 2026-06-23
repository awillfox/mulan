package domain

import "testing"

func TestValidRole(t *testing.T) {
	tests := []struct {
		name string
		role string
		want bool
	}{
		{"owner ok", "owner", true},
		{"staff ok", "staff", true},
		{"empty rejected", "", false},
		{"unknown rejected", "admin", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidRole(tc.role); got != tc.want {
				t.Errorf("ValidRole(%q) = %v, want %v", tc.role, got, tc.want)
			}
		})
	}
}

func TestGenerateTokenAndHashAreStable(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(tok) < 32 {
		t.Errorf("token too short: %d", len(tok))
	}
	h1 := HashToken(tok)
	h2 := HashToken(tok)
	if h1 != h2 {
		t.Errorf("HashToken not stable: %q vs %q", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("hash len = %d, want 64 (sha256 hex)", len(h1))
	}
	if h1 == tok {
		t.Errorf("hash must differ from token")
	}
}
