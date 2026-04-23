package http

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"mulan/internal/settings/service"
)

type Handler struct {
	svc *service.SettingsService
}

func NewHandler(svc *service.SettingsService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.Get)
	r.Patch("/", h.Update)
}

type settingsResponse struct {
	ShopName   string  `json:"shop_name"`
	VATPercent float64 `json:"vat_percent"`
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	row := h.svc.Get()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settingsResponse{
		ShopName:   row.ShopName,
		VATPercent: row.VatPercent,
	})
}

type updateRequest struct {
	ShopName   string  `json:"shop_name"`
	VATPercent float64 `json:"vat_percent"`
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.ShopName == "" {
		http.Error(w, "shop_name is required", http.StatusBadRequest)
		return
	}
	if req.VATPercent < 0 || req.VATPercent > 100 {
		http.Error(w, "vat_percent must be between 0 and 100", http.StatusBadRequest)
		return
	}

	row, err := h.svc.Update(r.Context(), req.ShopName, req.VATPercent)
	if err != nil {
		http.Error(w, "failed to update settings", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(settingsResponse{
		ShopName:   row.ShopName,
		VATPercent: row.VatPercent,
	})
}
