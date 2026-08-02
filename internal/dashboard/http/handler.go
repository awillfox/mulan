package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"mulan/internal/dashboard/service"
	"mulan/internal/httpx"
	"mulan/internal/response"
)

// maxRangeDays caps how wide a from..to window the dashboard accepts.
// Wider ranges aggregate over the full order_items table and can produce
// hundreds of rows; clamp at the handler so a misbehaving client can't
// pull "all-time" by accident. One year + a leap day, so the manager's
// custom date picker can cover a full year.
const maxRangeDays = 366

// shopLocation is the IANA zone used for "today" defaults and as the
// bucket timezone in dashboard SQL. Kept consistent with dashboard.shopTZ.
var shopLocation = mustLoadLocation("Asia/Bangkok")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
}

type Handler struct {
	svc *service.DashboardService
}

func NewHandler(svc *service.DashboardService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.Summary)
	r.Get("/top-menus", h.TopMenus)
	r.Get("/menu-items", h.MenuItems)
	r.Get("/sales-by-day", h.SalesByDay)
	r.Get("/heatmap", h.Heatmap)
	r.Get("/compare", h.Compare)
	r.Get("/subsidies", h.Subsidies)
}

// rangeFromQuery defaults to today (shop-local 00:00 → next 00:00) and
// overrides with `from` / `to` ISO date query params (inclusive day,
// exclusive end). Returns 400 if the resulting window exceeds maxRangeDays.
func rangeFromQuery(r *http.Request) (time.Time, time.Time, error) {
	now := time.Now().In(shopLocation)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shopLocation)
	from := today
	to := today.Add(24 * time.Hour)

	if t, ok, err := httpx.DateQuery(r, "from", shopLocation); err != nil {
		return time.Time{}, time.Time{}, err
	} else if ok {
		from = t
	}
	if t, ok, err := httpx.DateQuery(r, "to", shopLocation); err != nil {
		return time.Time{}, time.Time{}, err
	} else if ok {
		to = t.Add(24 * time.Hour)
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, errors.New("to must be after from")
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		return time.Time{}, time.Time{}, errors.New("range too large")
	}
	return from, to, nil
}

func (h *Handler) SalesByDay(w http.ResponseWriter, r *http.Request) {
	from, to, err := rangeFromQuery(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	out, err := h.svc.SalesByDay(r.Context(), from, to)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to load sales by day", err)
		return
	}
	response.OK(w, r, out)
}

func (h *Handler) Heatmap(w http.ResponseWriter, r *http.Request) {
	from, to, err := rangeFromQuery(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	out, err := h.svc.Heatmap(r.Context(), from, to)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to load heatmap", err)
		return
	}
	response.OK(w, r, out)
}

func (h *Handler) Compare(w http.ResponseWriter, r *http.Request) {
	from, to, err := rangeFromQuery(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	out, err := h.svc.Compare(r.Context(), from, to)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to compare periods", err)
		return
	}
	response.OK(w, r, out)
}

func (h *Handler) Summary(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.TodaySummary(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to load summary", err)
		return
	}
	response.OK(w, r, out)
}

// menuList serves an aggregated menu-sales list: parse the window, call
// load, envelope the result. Shared by TopMenus and MenuItems, which
// differ only in whether the list is capped.
func (h *Handler) menuList(
	w http.ResponseWriter,
	r *http.Request,
	load func(context.Context, time.Time, time.Time) ([]service.TopMenuItem, error),
	failMsg string,
) {
	from, to, err := rangeFromQuery(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	items, err := load(r.Context(), from, to)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, failMsg, err)
		return
	}
	response.OK(w, r, items)
}

// TopMenus returns the capped list behind the dashboard's item-mix donut.
func (h *Handler) TopMenus(w http.ResponseWriter, r *http.Request) {
	h.menuList(w, r, h.svc.TopMenus, "failed to load top menus")
}

// MenuItems returns every item sold in the window, behind the dashboard's
// "All items" list.
func (h *Handler) MenuItems(w http.ResponseWriter, r *http.Request) {
	h.menuList(w, r, h.svc.MenuItems, "failed to load menu items")
}

func (h *Handler) Subsidies(w http.ResponseWriter, r *http.Request) {
	from, to, err := rangeFromQuery(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	items, err := h.svc.SubsidyByProgram(r.Context(), from, to)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to load subsidies", err)
		return
	}
	response.OK(w, r, items)
}
