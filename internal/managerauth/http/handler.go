package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"mulan/internal/managerauth/domain"
	"mulan/internal/managerauth/service"
	"mulan/internal/response"
)

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// Routes registers ONLY the public login. Logout + Me require the bearer and are
// registered by main.go inside the RequireManager-protected group.
func (h *Handler) Routes(r chi.Router) {
	r.Post("/login", h.login)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	Token     string      `json:"token"`
	ExpiresAt time.Time   `json:"expires_at"`
	User      domain.User `json:"user"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if req.Username == "" || req.Password == "" {
		response.Error(w, r, http.StatusBadRequest, "username and password required", nil)
		return
	}
	user, token, expires, err := h.svc.Login(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			response.Error(w, r, http.StatusUnauthorized, "invalid credentials", err)
			return
		}
		response.Error(w, r, http.StatusInternalServerError, "login failed", err)
		return
	}
	response.OK(w, r, loginResponse{Token: token, ExpiresAt: expires, User: user})
}

// Logout revokes the caller's current session. Registered under RequireManager.
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	token := BearerToken(r)
	if err := h.svc.Logout(r.Context(), token); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "logout failed", err)
		return
	}
	response.NoContent(w, r)
}

// Me returns the authenticated user pulled from request context by the middleware.
func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		response.Error(w, r, http.StatusUnauthorized, "not authenticated", nil)
		return
	}
	response.OK(w, r, user)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword lets the authenticated user change their own password after
// confirming their current one. Registered under RequireManager.
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok {
		response.Error(w, r, http.StatusUnauthorized, "not authenticated", nil)
		return
	}
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		response.Error(w, r, http.StatusBadRequest, "current_password and new_password required", nil)
		return
	}
	err := h.svc.ChangePassword(r.Context(), user.Username, req.CurrentPassword, req.NewPassword)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			response.Error(w, r, http.StatusUnauthorized, "current password is incorrect", err)
		case errors.Is(err, service.ErrPasswordTooShort):
			response.Error(w, r, http.StatusBadRequest, "new password must be at least 8 characters", err)
		default:
			response.Error(w, r, http.StatusInternalServerError, "failed to change password", err)
		}
		return
	}
	response.NoContent(w, r)
}
