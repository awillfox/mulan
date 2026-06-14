package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"mulan/internal/httpx"
	"mulan/internal/report/service"
	"mulan/internal/response"
)

const (
	maxRangeDays = 92
	defaultLimit = 100
	maxLimit     = 200
)

var (
	errBadRange      = errors.New("to must be after from")
	errRangeTooLarge = errors.New("range too large")
	errBadStatus     = errors.New("invalid status")
	errBadLimit      = errors.New("invalid limit")
	errBadOffset     = errors.New("invalid offset")
)

var shopLocation = mustLoad("Asia/Bangkok")

func mustLoad(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.Local
	}
	return loc
}

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/orders", h.ListOrders)
}

// parseListParams reads from/to (shop-local ISO dates, default last 7 days),
// status (paid|open|held, default all), and limit/offset.
func parseListParams(r *http.Request) (service.ListParams, error) {
	now := time.Now().In(shopLocation)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, shopLocation)
	from := today.AddDate(0, 0, -6)
	to := today.Add(24 * time.Hour)

	if t, ok, err := httpx.DateQuery(r, "from", shopLocation); err != nil {
		return service.ListParams{}, err
	} else if ok {
		from = t
	}
	if t, ok, err := httpx.DateQuery(r, "to", shopLocation); err != nil {
		return service.ListParams{}, err
	} else if ok {
		to = t.Add(24 * time.Hour)
	}
	if !to.After(from) {
		return service.ListParams{}, errBadRange
	}
	if to.Sub(from) > maxRangeDays*24*time.Hour {
		return service.ListParams{}, errRangeTooLarge
	}

	status := r.URL.Query().Get("status")
	switch status {
	case "", "paid", "open", "held":
	default:
		return service.ListParams{}, errBadStatus
	}

	limit := int32(defaultLimit)
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			return service.ListParams{}, errBadLimit
		}
		if n > maxLimit {
			n = maxLimit
		}
		limit = int32(n)
	}
	var offset int32
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return service.ListParams{}, errBadOffset
		}
		offset = int32(n)
	}

	return service.ListParams{Status: status, From: from, To: to, Limit: limit, Offset: offset}, nil
}

func (h *Handler) ListOrders(w http.ResponseWriter, r *http.Request) {
	p, err := parseListParams(r)
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	page, err := h.svc.ListOrders(r.Context(), p)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to load orders", err)
		return
	}
	response.OK(w, r, page)
}
