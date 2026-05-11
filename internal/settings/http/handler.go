package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"mulan/internal/response"
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
	response.OK(w, r, settingsResponse{
		ShopName:   row.ShopName,
		VATPercent: row.VatPercent,
	})
}

type updateRequest struct {
	ShopName   string  `json:"shop_name"`
	VATPercent float64 `json:"vat_percent"`
}

func (req updateRequest) validate() error {
	if req.ShopName == "" {
		return errors.New("shop_name is required")
	}
	if req.VATPercent < 0 || req.VATPercent > 100 {
		return errors.New("vat_percent must be between 0 and 100")
	}
	return nil
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}

	row, err := h.svc.Update(r.Context(), req.ShopName, req.VATPercent)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to update settings", err)
		return
	}

	response.OK(w, r, settingsResponse{
		ShopName:   row.ShopName,
		VATPercent: row.VatPercent,
	})
}
