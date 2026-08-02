package http

import (
	"net/http/httptest"
	"testing"
)

func rangeDays(t *testing.T, from, to string) (float64, error) {
	t.Helper()
	req := httptest.NewRequest("GET", "/dashboard/menu-items?from="+from+"&to="+to, nil)
	f, tt, err := rangeFromQuery(req)
	if err != nil {
		return 0, err
	}
	return tt.Sub(f).Hours() / 24, nil
}

func TestRangeFromQueryAcceptsOneYear(t *testing.T) {
	// 2025-08-02..2026-08-01 inclusive = 365 days.
	days, err := rangeDays(t, "2025-08-02", "2026-08-01")
	if err != nil {
		t.Fatalf("expected 365-day range to be accepted, got error: %v", err)
	}
	if days != 365 {
		t.Fatalf("expected 365 days, got %v", days)
	}
}

func TestRangeFromQueryAcceptsExactly366Days(t *testing.T) {
	// 2025-08-02..2026-08-02 inclusive = 366 days, the exact ceiling.
	if _, err := rangeDays(t, "2025-08-02", "2026-08-02"); err != nil {
		t.Fatalf("expected 366-day range to be accepted, got error: %v", err)
	}
}

func TestRangeFromQueryRejects367Days(t *testing.T) {
	// 2025-08-02..2026-08-03 inclusive = 367 days, one past the ceiling.
	if _, err := rangeDays(t, "2025-08-02", "2026-08-03"); err == nil {
		t.Fatal("expected 367-day range to be rejected, got nil error")
	}
}

func TestRangeFromQueryRejectsReversedRange(t *testing.T) {
	if _, err := rangeDays(t, "2026-08-02", "2026-08-01"); err == nil {
		t.Fatal("expected reversed range to be rejected, got nil error")
	}
}
