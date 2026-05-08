package http

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"mulan/internal/dashboard/service"
	"mulan/internal/httpx"
	"mulan/internal/response"
)

type Handler struct {
	svc *service.DashboardService
}

func NewHandler(svc *service.DashboardService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.Summary)
	r.Get("/top-menus", h.TopMenus)
	r.Get("/sales-by-day", h.SalesByDay)
	r.Get("/heatmap", h.Heatmap)
	r.Get("/compare", h.Compare)
}

// rangeFromQuery defaults to today (00:00 → next 00:00) and overrides with
// `from` / `to` ISO date query params (inclusive day, exclusive end).
func rangeFromQuery(r *http.Request) (time.Time, time.Time, error) {
	loc := time.Local
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	from := today
	to := today.Add(24 * time.Hour)

	if t, ok, err := httpx.DateQuery(r, "from", loc); err != nil {
		return time.Time{}, time.Time{}, err
	} else if ok {
		from = t
	}
	if t, ok, err := httpx.DateQuery(r, "to", loc); err != nil {
		return time.Time{}, time.Time{}, err
	} else if ok {
		to = t.Add(24 * time.Hour)
	}
	return from, to, nil
}

func (h *Handler) SalesByDay(w http.ResponseWriter, r *http.Request) {
	from, to, err := rangeFromQuery(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.SalesByDay(r.Context(), from, to)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("sales by day: %w", err))
		return
	}
	response.OK(w, r, out)
}

func (h *Handler) Heatmap(w http.ResponseWriter, r *http.Request) {
	from, to, err := rangeFromQuery(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.Heatmap(r.Context(), from, to)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("heatmap: %w", err))
		return
	}
	response.OK(w, r, out)
}

func (h *Handler) Compare(w http.ResponseWriter, r *http.Request) {
	from, to, err := rangeFromQuery(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err)
		return
	}
	out, err := h.svc.Compare(r.Context(), from, to)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("compare: %w", err))
		return
	}
	response.OK(w, r, out)
}

func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.TodaySummary(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("summary: %w", err))
		return
	}
	response.OK(w, r, out)
}

func (h *Handler) TopMenus(w http.ResponseWriter, r *http.Request) {
	from, to, err := rangeFromQuery(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err)
		return
	}
	items, err := h.svc.TopMenus(r.Context(), from, to)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("top menus: %w", err))
		return
	}
	response.OK(w, r, items)
}
