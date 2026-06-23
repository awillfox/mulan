package service

import "testing"

// validRole mirrors the DB CHECK so the service rejects bad roles before hitting
// Postgres. Kept as a tiny pure helper so it is unit-testable without a DB.
func TestValidRole(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"cashier", true},
		{"manager", true},
		{"owner", false},
		{"", false},
		{"Manager", false},
	}
	for _, tc := range cases {
		if got := validRole(tc.in); got != tc.want {
			t.Errorf("validRole(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}
