package http

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"mulan/internal/baseoption/service"
	"mulan/internal/httpx"
	"mulan/internal/response"
)

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

type baseOptionInput struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"` // THB
}

type setBaseOptionsRequest struct {
	BaseOptions []baseOptionInput `json:"base_options"`
}

// satangFromTHB converts a client THB amount to integer satang with bank-style
// rounding so 0.07 doesn't truncate.
func satangFromTHB(thb float64) int64 {
	return int64(math.Round(thb * 100))
}

// SetMenuBaseOptions replaces the menu's base options. PUT /api/menus/{id}/base-options
func (h *Handler) SetMenuBaseOptions(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	var req setBaseOptionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	specs := make([]service.Spec, 0, len(req.BaseOptions))
	for _, b := range req.BaseOptions {
		if b.Name == "" {
			continue
		}
		if b.Price < 0 {
			response.Error(w, r, http.StatusBadRequest, "price must be >= 0", errors.New("negative price"))
			return
		}
		specs = append(specs, service.Spec{Name: b.Name, Price: satangFromTHB(b.Price)})
	}
	if err := h.svc.SetForMenu(r.Context(), id, specs); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to set base options", err)
		return
	}
	response.NoContent(w, r)
}
