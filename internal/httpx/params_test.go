package httpx

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestURLParamInt32(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    int32
		wantErr bool
	}{
		{"valid", "42", 42, false},
		{"negative", "-5", -5, false},
		{"empty", "", 0, true},
		{"non-numeric", "abc", 0, true},
		{"overflow", "99999999999", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/", nil)
			rctx := chi.NewRouteContext()
			if tc.raw != "" {
				rctx.URLParams.Add("id", tc.raw)
			}
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))

			got, err := URLParamInt32(r, "id")
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestDateQuery(t *testing.T) {
	loc := time.UTC
	tests := []struct {
		name    string
		query   string
		wantOk  bool
		wantErr bool
		want    time.Time
	}{
		{"missing", "", false, false, time.Time{}},
		{"valid", "from=2026-01-15", true, false, time.Date(2026, 1, 15, 0, 0, 0, 0, loc)},
		{"bad format", "from=01/15/2026", false, true, time.Time{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/?"+tc.query, nil)
			got, ok, err := DateQuery(r, "from", loc)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if ok != tc.wantOk {
				t.Fatalf("ok: got %v want %v", ok, tc.wantOk)
			}
			if !got.Equal(tc.want) {
				t.Fatalf("time: got %v want %v", got, tc.want)
			}
		})
	}
}
