package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Rhymond/go-money"

	"mulan/internal/httpx"
	"mulan/internal/member/domain"
	"mulan/internal/member/service"
	"mulan/internal/response"
	"mulan/sqlc"
)

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

type memberResponse struct {
	ID        int32  `json:"id"`
	Phone     string `json:"phone"`
	Name      string `json:"name"`
	Points    int64  `json:"points"`
	CreatedAt string `json:"created_at,omitempty"`
}

func toMemberResponse(m sqlc.Member) memberResponse {
	out := memberResponse{
		ID:     m.ID,
		Phone:  m.Phone,
		Name:   m.Name.String,
		Points: m.Points,
	}
	if m.CreatedAt.Valid {
		out.CreatedAt = m.CreatedAt.Time.Format(time.RFC3339)
	}
	return out
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	members, err := h.svc.List(r.Context(), r.URL.Query().Get("q"))
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to list members", err)
		return
	}
	out := make([]memberResponse, len(members))
	for i, m := range members {
		out[i] = toMemberResponse(m)
	}
	response.OK(w, r, out)
}

func (h *Handler) Lookup(w http.ResponseWriter, r *http.Request) {
	phone := strings.TrimSpace(r.URL.Query().Get("phone"))
	if phone == "" {
		response.Error(w, r, http.StatusBadRequest, "phone is required", nil)
		return
	}
	m, err := h.svc.Lookup(r.Context(), phone)
	if errors.Is(err, service.ErrNotFound) {
		response.Error(w, r, http.StatusNotFound, "member not found", err)
		return
	}
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to lookup member", err)
		return
	}
	response.OK(w, r, toMemberResponse(m))
}

type memberRequest struct {
	Phone string `json:"phone"`
	Name  string `json:"name"`
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req memberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	phone := domain.NormalizePhone(req.Phone)
	if err := domain.ValidatePhone(phone); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	m, err := h.svc.Create(r.Context(), phone, strings.TrimSpace(req.Name))
	if errors.Is(err, service.ErrDuplicatePhone) {
		response.Error(w, r, http.StatusConflict, "phone already registered", err)
		return
	}
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to create member", err)
		return
	}
	response.Created(w, r, toMemberResponse(m))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	var req memberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	phone := domain.NormalizePhone(req.Phone)
	if err := domain.ValidatePhone(phone); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	m, err := h.svc.Update(r.Context(), id, phone, strings.TrimSpace(req.Name))
	if errors.Is(err, service.ErrDuplicatePhone) {
		response.Error(w, r, http.StatusConflict, "phone already registered", err)
		return
	}
	if errors.Is(err, service.ErrNotFound) {
		response.Error(w, r, http.StatusNotFound, "member not found", err)
		return
	}
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to update member", err)
		return
	}
	response.OK(w, r, toMemberResponse(m))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to delete member", err)
		return
	}
	response.NoContent(w, r)
}

type memberOrderResponse struct {
	Code         string  `json:"code"`
	CreatedAt    string  `json:"created_at"`
	PointsEarned int64   `json:"points_earned"`
	Subtotal     float64 `json:"subtotal"` // THB, goods total excluding VAT
}

func (h *Handler) Orders(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	rows, err := h.svc.Orders(r.Context(), id)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to list member orders", err)
		return
	}
	out := make([]memberOrderResponse, len(rows))
	for i, o := range rows {
		mo := memberOrderResponse{
			Code:         o.Code,
			PointsEarned: o.PointsEarned,
			Subtotal:     money.New(o.Subtotal, money.THB).AsMajorUnits(),
		}
		if o.CreatedAt.Valid {
			mo.CreatedAt = o.CreatedAt.Time.Format(time.RFC3339)
		}
		out[i] = mo
	}
	response.OK(w, r, out)
}
