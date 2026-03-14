package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"mulan/internal/order/domain"
	"mulan/internal/order/service"
)

type Handler struct {
	svc *service.OrderService
}

func NewHandler(svc *service.OrderService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/", h.create)
	r.Post("/{code}/checkout", h.checkout)
}

func (h *Handler) TopMenus(w http.ResponseWriter, r *http.Request) {
	loc := time.Local
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	from := today
	to := today.Add(24 * time.Hour)

	if v := r.URL.Query().Get("from"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
			from = t
		}
	}
	if v := r.URL.Query().Get("to"); v != "" {
		if t, err := time.ParseInLocation("2006-01-02", v, loc); err == nil {
			to = t.Add(24 * time.Hour)
		}
	}

	items, err := h.svc.TopMenus(r.Context(), from, to)
	if err != nil {
		http.Error(w, "failed to get top menus", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

func (h *Handler) DashboardSummary(w http.ResponseWriter, r *http.Request) {
	summary, err := h.svc.TodaySummary(r.Context())
	if err != nil {
		http.Error(w, "failed to get summary", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(summary)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	code, err := h.svc.Create(r.Context())
	if err != nil {
		http.Error(w, "failed to create order", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"code": code})
}

type checkoutItemRequest struct {
	MenuID int32   `json:"menu_id"`
	Name   string  `json:"name"`
	Price  float64 `json:"price"` // THB from frontend
	Qty    int32   `json:"qty"`
}

type checkoutRequest struct {
	Items []checkoutItemRequest `json:"items"`
}

func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	var req checkoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(req.Items) == 0 {
		http.Error(w, "no items", http.StatusBadRequest)
		return
	}

	items := make([]domain.OrderItem, len(req.Items))
	for i, it := range req.Items {
		items[i] = domain.OrderItem{
			MenuID: it.MenuID,
			Name:   it.Name,
			Price:  int64(it.Price * 100), // THB → satang
			Qty:    it.Qty,
		}
	}

	result, err := h.svc.Checkout(r.Context(), code, items)
	if err != nil {
		http.Error(w, "checkout failed", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"code":     result.Code,
		"subtotal": result.Subtotal,
		"vat":      result.VAT,
		"total":    result.Total,
	})
}
