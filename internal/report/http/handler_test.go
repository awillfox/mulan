package http

import (
	"net/http/httptest"
	"testing"

	"mulan/internal/report/service"
)

func TestParseListParams(t *testing.T) {
	mk := func(q string) (service.ListParams, error) {
		r := httptest.NewRequest("GET", "/orders"+q, nil)
		return parseListParams(r)
	}

	if _, err := mk(""); err != nil {
		t.Fatalf("defaults should parse: %v", err)
	}
	if _, err := mk("?status=bogus"); err == nil {
		t.Fatal("want error for bad status")
	}
	if _, err := mk("?from=2026-01-01&to=2026-12-31"); err == nil {
		t.Fatal("want error for range too large")
	}
	p, err := mk("?limit=9999")
	if err != nil {
		t.Fatalf("limit parse: %v", err)
	}
	if p.Limit != maxLimit {
		t.Fatalf("want limit clamped to %d, got %d", maxLimit, p.Limit)
	}
	if _, err := mk("?offset=-1"); err == nil {
		t.Fatal("want error for negative offset")
	}
}
