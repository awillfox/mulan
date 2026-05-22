package http

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"github.com/go-chi/chi/v5"

	"mulan/internal/discount/domain"
	"mulan/internal/discount/service"
	"mulan/internal/httpx"
	"mulan/internal/hub"
	"mulan/internal/response"
	"mulan/sqlc"
)

type Handler struct {
	svc *service.Service
	hub *hub.Hub
}

func NewHandler(svc *service.Service, h *hub.Hub) *Handler {
	return &Handler{svc: svc, hub: h}
}

// notifyChange tells every connected POS to refetch its discount list. The
// payload is intentionally empty — the POS just re-pulls /api/discounts/active
// so it always sees the authoritative set (created, edited, enabled, disabled,
// or deleted) without us having to diff anything here.
func (h *Handler) notifyChange() {
	if h.hub != nil {
		h.hub.Broadcast("discounts_changed", "{}")
	}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.List)
	r.Get("/active", h.ListActive)
	r.Post("/", h.Create)
	r.Patch("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
}

// discountResponse exposes value as a human-readable number: THB for fixed
// discounts, percent for percentage discounts. The DB stores both scaled by
// 100 (satang / hundredths-of-percent) so the wire value is always Value/100.
type discountResponse struct {
	ID           int32   `json:"id"`
	Name         string  `json:"name"`
	DiscountType string  `json:"discount_type"`
	Value        float64 `json:"value"`
	Active       bool    `json:"active"`
}

func toResponse(d sqlc.Discount) discountResponse {
	return discountResponse{
		ID:           d.ID,
		Name:         d.Name,
		DiscountType: d.DiscountType,
		Value:        float64(d.Value) / 100,
		Active:       d.Active,
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.List(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to list discounts", err)
		return
	}
	out := make([]discountResponse, len(rows))
	for i, d := range rows {
		out[i] = toResponse(d)
	}
	response.OK(w, r, out)
}

func (h *Handler) ListActive(w http.ResponseWriter, r *http.Request) {
	rows, err := h.svc.ListActive(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to list discounts", err)
		return
	}
	out := make([]discountResponse, len(rows))
	for i, d := range rows {
		out[i] = toResponse(d)
	}
	response.OK(w, r, out)
}

type discountRequest struct {
	Name         string  `json:"name"`
	DiscountType string  `json:"discount_type"`
	Value        float64 `json:"value"`
	Active       *bool   `json:"active"`
}

func (req discountRequest) validate() error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	if !domain.ValidType(req.DiscountType) {
		return errors.New("invalid discount_type")
	}
	if req.Value < 0 {
		return errors.New("value must not be negative")
	}
	if req.DiscountType == domain.TypePercent && req.Value > 100 {
		return errors.New("percent discount cannot exceed 100")
	}
	return nil
}

// scaledValue converts the wire value to the int64 the DB stores: satang for
// fixed discounts, hundredths-of-percent for percentage discounts. Both are
// value*100, rounded bank-style so 0.07 doesn't truncate.
func scaledValue(v float64) int64 {
	return int64(math.Round(v * 100))
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req discountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	d, err := h.svc.Create(r.Context(), req.Name, req.DiscountType, scaledValue(req.Value), active)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to create discount", err)
		return
	}
	h.notifyChange()
	response.Created(w, r, toResponse(d))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	var req discountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	d, err := h.svc.Update(r.Context(), id, req.Name, req.DiscountType, scaledValue(req.Value), active)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to update discount", err)
		return
	}
	h.notifyChange()
	response.OK(w, r, toResponse(d))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to delete discount", err)
		return
	}
	h.notifyChange()
	response.NoContent(w, r)
}
