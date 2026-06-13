package http

import (
	"context"
	"net/http"
	"strings"

	"mulan/internal/managerauth/domain"
	"mulan/internal/managerauth/service"
	"mulan/internal/response"
)

type ctxKey int

const userKey ctxKey = 0

// BearerToken extracts the token from an "Authorization: Bearer <token>" header.
// Returns "" when absent or malformed.
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):])
	}
	return ""
}

// UserFromContext returns the authenticated user stored by RequireManager.
func UserFromContext(ctx context.Context) (domain.User, bool) {
	u, ok := ctx.Value(userKey).(domain.User)
	return u, ok
}

// RequireManager validates the bearer token and stores the user in context,
// responding 401 on any failure.
func RequireManager(svc *service.Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, err := svc.Authenticate(r.Context(), BearerToken(r))
			if err != nil {
				response.Error(w, r, http.StatusUnauthorized, "authentication required", err)
				return
			}
			ctx := context.WithValue(r.Context(), userKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole gates a handler to the listed roles (use AFTER RequireManager).
// Responds 403 when the user's role is not allowed.
func RequireRole(roles ...string) func(http.Handler) http.Handler {
	allowed := make(map[string]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			user, ok := UserFromContext(r.Context())
			if !ok || !allowed[user.Role] {
				response.Error(w, r, http.StatusForbidden, "insufficient permissions", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
