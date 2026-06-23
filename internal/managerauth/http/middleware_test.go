package http

import (
	"context"
	"net/http"
	"testing"

	"mulan/internal/managerauth/domain"
)

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{"valid", "Bearer abc.def", "abc.def"},
		{"case-insensitive scheme", "bearer xyz", "xyz"},
		{"no scheme", "abc", ""},
		{"empty", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := http.NewRequest("GET", "/", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			if got := BearerToken(r); got != tc.want {
				t.Errorf("BearerToken(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

func TestUserFromContext(t *testing.T) {
	want := domain.User{ID: 7, Username: "owner", Role: domain.RoleOwner}
	ctx := context.WithValue(context.Background(), userKey, want)
	got, ok := UserFromContext(ctx)
	if !ok || got != want {
		t.Errorf("UserFromContext = %+v, %v; want %+v, true", got, ok, want)
	}
	if _, ok := UserFromContext(context.Background()); ok {
		t.Errorf("UserFromContext on empty ctx returned ok=true")
	}
}
