package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"mulan/internal/cashier/service"
	"mulan/internal/response"
)

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Post("/login", h.Login)
	r.Patch("/{id}", h.Update)
	r.Patch("/{id}/pin", h.UpdatePin)
	r.Delete("/{id}", h.Delete)
}

type cashierResponse struct {
	ID       int32  `json:"id"`
	LoginID  string `json:"login_id"`
	Name     string `json:"name"`
	Active   bool   `json:"active"`
}

type loginRequest struct {
	LoginID string `json:"login_id"`
	PIN     string `json:"pin"`
}

type createRequest struct {
	LoginID string `json:"login_id"`
	Name    string `json:"name"`
	PIN     string `json:"pin"`
}

type updateRequest struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

type updatePinRequest struct {
	PIN string `json:"pin"`
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if req.LoginID == "" || req.PIN == "" {
		response.Error(w, r, http.StatusBadRequest, "login_id and pin required", nil)
		return
	}
	c, err := h.svc.Login(r.Context(), req.LoginID, req.PIN)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			response.Error(w, r, http.StatusUnauthorized, "invalid credentials", err)
			return
		}
		response.Error(w, r, http.StatusInternalServerError, "login failed", err)
		return
	}
	response.OK(w, r, cashierResponse{ID: c.ID, LoginID: c.LoginID, Name: c.Name, Active: c.Active})
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	cashiers, err := h.svc.List(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to list cashiers", err)
		return
	}
	out := make([]cashierResponse, len(cashiers))
	for i, c := range cashiers {
		out[i] = cashierResponse{ID: c.ID, LoginID: c.LoginID, Name: c.Name, Active: c.Active}
	}
	response.OK(w, r, out)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if req.LoginID == "" || req.Name == "" || req.PIN == "" {
		response.Error(w, r, http.StatusBadRequest, "login_id, name, and pin are required", nil)
		return
	}
	if len(req.PIN) < 4 {
		response.Error(w, r, http.StatusBadRequest, "pin must be at least 4 digits", nil)
		return
	}
	c, err := h.svc.Create(r.Context(), req.LoginID, req.Name, req.PIN)
	if err != nil {
		if errors.Is(err, service.ErrLoginIDTaken) {
			response.Error(w, r, http.StatusConflict, "login ID already in use", err)
			return
		}
		response.Error(w, r, http.StatusInternalServerError, "failed to create cashier", err)
		return
	}
	response.Created(w, r, cashierResponse{ID: c.ID, LoginID: c.LoginID, Name: c.Name, Active: c.Active})
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid id", err)
		return
	}
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if req.Name == "" {
		response.Error(w, r, http.StatusBadRequest, "name is required", nil)
		return
	}
	c, err := h.svc.Update(r.Context(), int32(id), req.Name, req.Active)
	if err != nil {
		if errors.Is(err, service.ErrCashierNotFound) {
			response.Error(w, r, http.StatusNotFound, "cashier not found", err)
			return
		}
		response.Error(w, r, http.StatusInternalServerError, "failed to update cashier", err)
		return
	}
	response.OK(w, r, cashierResponse{ID: c.ID, LoginID: c.LoginID, Name: c.Name, Active: c.Active})
}

func (h *Handler) UpdatePin(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid id", err)
		return
	}
	var req updatePinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if len(req.PIN) < 4 {
		response.Error(w, r, http.StatusBadRequest, "pin must be at least 4 digits", nil)
		return
	}
	if err := h.svc.UpdatePin(r.Context(), int32(id), req.PIN); err != nil {
		if errors.Is(err, service.ErrCashierNotFound) {
			response.Error(w, r, http.StatusNotFound, "cashier not found", err)
			return
		}
		response.Error(w, r, http.StatusInternalServerError, "failed to update pin", err)
		return
	}
	response.NoContent(w, r)
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid id", err)
		return
	}
	if err := h.svc.Delete(r.Context(), int32(id)); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to delete cashier", err)
		return
	}
	response.NoContent(w, r)
}
