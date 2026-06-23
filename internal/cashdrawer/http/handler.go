// Package http exposes the cash drawer state + audit log over HTTP.
//
// JSON convention follows the rest of the project: client-facing amounts are
// in THB (float), server-side storage stays in satang (int64).
package http

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strconv"

	"github.com/Rhymond/go-money"
	"github.com/go-chi/chi/v5"

	"mulan/internal/cashdrawer/service"
	"mulan/internal/response"
)

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

// Routes registers the open (POS/agent-shared) cash-drawer endpoints. The
// denomination WRITES are owner-gated and registered separately via OwnerRoutes.
func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.current)
	r.Put("/float", h.setFloat)
	r.Delete("/float", h.clearFloat)
	r.Post("/kick", h.logKick)
	r.Get("/audit", h.listAudit)
	r.Get("/denominations", h.getDenominations)
	r.Post("/change-preview", h.changePreview)
}

// OwnerRoutes registers the denomination write endpoints. main.go mounts these
// under the same /cash-drawer prefix but wrapped in RequireRole(owner), so only
// manager-auth owners can set/adjust the drawer's bill/coin counts.
func (h *Handler) OwnerRoutes(r chi.Router) {
	r.Put("/denominations", h.SetDenominations)
	r.Post("/denominations/adjust", h.AdjustDenominations)
}

// ── DTOs ────────────────────────────────────────────────────────────

type currentResponse struct {
	Amount      float64 `json:"amount"`
	AmountValid bool    `json:"amount_valid"`
	EventType   string  `json:"event_type,omitempty"`
	SetAt       string  `json:"set_at,omitempty"`
}

type setFloatRequest struct {
	Amount   float64 `json:"amount"`
	Note     *string `json:"note,omitempty"`
	Actor    *string `json:"actor,omitempty"`
	Terminal *string `json:"terminal,omitempty"`
}

type kickRequest struct {
	Reason   string  `json:"reason,omitempty"` // "manual" | "change" — maps to event_type
	Note     *string `json:"note,omitempty"`
	Actor    *string `json:"actor,omitempty"`
	Terminal *string `json:"terminal,omitempty"`
}

type clearRequest struct {
	Note     *string `json:"note,omitempty"`
	Actor    *string `json:"actor,omitempty"`
	Terminal *string `json:"terminal,omitempty"`
}

type auditEventResponse struct {
	ID        int64    `json:"id"`
	EventType string   `json:"event_type"`
	Amount    *float64 `json:"amount,omitempty"`
	Delta     *float64 `json:"delta,omitempty"`
	Note      *string  `json:"note,omitempty"`
	Actor     *string  `json:"actor,omitempty"`
	Terminal  *string  `json:"terminal,omitempty"`
	CreatedAt string   `json:"created_at"`
}

type auditListResponse struct {
	Total  int64                `json:"total"`
	Limit  int32                `json:"limit"`
	Offset int32                `json:"offset"`
	Events []auditEventResponse `json:"events"`
}

// ── Handlers ───────────────────────────────────────────────────────

func (h *Handler) current(w http.ResponseWriter, r *http.Request) {
	cur, err := h.svc.Current(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to read cash drawer", err)
		return
	}
	out := currentResponse{
		AmountValid: cur.AmountValid,
		EventType:   cur.EventType,
	}
	if cur.AmountValid {
		out.Amount = money.New(cur.Amount, money.THB).AsMajorUnits()
	}
	if cur.SetAt.Valid {
		out.SetAt = cur.SetAt.Time.UTC().Format("2006-01-02T15:04:05Z")
	}
	response.OK(w, r, out)
}

// satangFromTHB rounds a client-supplied amount to integer satang. Same
// rationale as the menu handler: direct truncation drops a satang on values
// like 7.07 because the IEEE-754 representation sits a hair below 7×10⁻².
func satangFromTHB(thb float64) int64 {
	return int64(math.Round(thb * 100))
}

func (h *Handler) setFloat(w http.ResponseWriter, r *http.Request) {
	var req setFloatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if req.Amount < 0 {
		response.Error(w, r, http.StatusBadRequest, "amount must be >= 0", nil)
		return
	}
	evt, err := h.svc.SetFloat(r.Context(), satangFromTHB(req.Amount), req.Note, req.Actor, req.Terminal)
	if err != nil {
		if errors.Is(err, service.ErrInvalidAmount) {
			response.Error(w, r, http.StatusBadRequest, err.Error(), err)
			return
		}
		response.Error(w, r, http.StatusInternalServerError, "failed to set float", err)
		return
	}
	response.OK(w, r, toAuditResponse(evt))
}

func (h *Handler) clearFloat(w http.ResponseWriter, r *http.Request) {
	var req clearRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // body optional on DELETE
	evt, err := h.svc.ClearFloat(r.Context(), req.Note, req.Actor, req.Terminal)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to clear float", err)
		return
	}
	response.OK(w, r, toAuditResponse(evt))
}

func (h *Handler) logKick(w http.ResponseWriter, r *http.Request) {
	var req kickRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	eventType := service.EventKick
	if req.Reason == "change" {
		eventType = service.EventOpenForChange
	}
	evt, err := h.svc.LogKick(r.Context(), eventType, req.Note, req.Actor, req.Terminal)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to log kick", err)
		return
	}
	response.OK(w, r, toAuditResponse(evt))
}

func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) {
	limit := parseIntQuery(r, "limit", 50)
	offset := parseIntQuery(r, "offset", 0)
	events, total, err := h.svc.ListAudit(r.Context(), int32(limit), int32(offset))
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to list audit", err)
		return
	}
	out := auditListResponse{Total: total, Limit: int32(limit), Offset: int32(offset), Events: make([]auditEventResponse, len(events))}
	for i, e := range events {
		out.Events[i] = toAuditResponse(e)
	}
	response.OK(w, r, out)
}

func parseIntQuery(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func toAuditResponse(e service.AuditEvent) auditEventResponse {
	out := auditEventResponse{
		ID:        e.ID,
		EventType: e.EventType,
		Note:      e.Note,
		Actor:     e.Actor,
		Terminal:  e.Terminal,
	}
	if e.Amount != nil {
		v := money.New(*e.Amount, money.THB).AsMajorUnits()
		out.Amount = &v
	}
	if e.Delta != nil {
		v := money.New(*e.Delta, money.THB).AsMajorUnits()
		out.Delta = &v
	}
	if e.CreatedAt.Valid {
		out.CreatedAt = e.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05Z")
	}
	return out
}
