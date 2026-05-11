package http

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"github.com/Rhymond/go-money"
	"github.com/go-chi/chi/v5"

	"mulan/internal/httpx"
	"mulan/internal/optiongroup/domain"
	"mulan/internal/optiongroup/service"
	"mulan/internal/response"
	"mulan/sqlc"
)

type Handler struct {
	svc *service.Service
}

func NewHandler(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.ListGroups)
	r.Post("/", h.CreateGroup)
	r.Patch("/{id}", h.UpdateGroup)
	r.Delete("/{id}", h.DeleteGroup)
	r.Post("/{id}/options", h.CreateOption)
}

func (h *Handler) OptionRoutes(r chi.Router) {
	r.Patch("/{id}", h.UpdateOption)
	r.Delete("/{id}", h.DeleteOption)
}

type optionResponse struct {
	ID         int32   `json:"id"`
	Name       string  `json:"name"`
	PriceDelta float64 `json:"price_delta"`
	SortOrder  int32   `json:"sort_order"`
}

type groupResponse struct {
	ID            int32            `json:"id"`
	Name          string           `json:"name"`
	SelectionMode string           `json:"selection_mode"`
	Options       []optionResponse `json:"options"`
}

func toOptionResponse(o sqlc.Option) optionResponse {
	return optionResponse{
		ID:         o.ID,
		Name:       o.Name,
		PriceDelta: money.New(o.PriceDelta, money.THB).AsMajorUnits(),
		SortOrder:  o.SortOrder,
	}
}

func toGroupResponse(g service.GroupWithOptions) groupResponse {
	out := groupResponse{
		ID:            g.Group.ID,
		Name:          g.Group.Name,
		SelectionMode: g.Group.SelectionMode,
		Options:       make([]optionResponse, len(g.Options)),
	}
	for i, o := range g.Options {
		out.Options[i] = toOptionResponse(o)
	}
	return out
}

func (h *Handler) ListGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := h.svc.ListGroups(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to list option groups", err)
		return
	}
	out := make([]groupResponse, len(groups))
	for i, g := range groups {
		out[i] = toGroupResponse(g)
	}
	response.OK(w, r, out)
}

type groupRequest struct {
	Name          string `json:"name"`
	SelectionMode string `json:"selection_mode"`
}

func (req groupRequest) validate() error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	if !domain.ValidSelectionMode(req.SelectionMode) {
		return errors.New("invalid selection_mode")
	}
	return nil
}

func (h *Handler) CreateGroup(w http.ResponseWriter, r *http.Request) {
	var req groupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	g, err := h.svc.CreateGroup(r.Context(), req.Name, req.SelectionMode)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to create group", err)
		return
	}
	response.Created(w, r, toGroupResponse(service.GroupWithOptions{Group: g}))
}

func (h *Handler) UpdateGroup(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	var req groupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	g, err := h.svc.UpdateGroup(r.Context(), id, req.Name, req.SelectionMode)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to update group", err)
		return
	}
	response.OK(w, r, toGroupResponse(service.GroupWithOptions{Group: g}))
}

func (h *Handler) DeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	if err := h.svc.DeleteGroup(r.Context(), id); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to delete group", err)
		return
	}
	response.NoContent(w, r)
}

type optionRequest struct {
	Name       string  `json:"name"`
	PriceDelta float64 `json:"price_delta"`
	SortOrder  int32   `json:"sort_order"`
}

func (req optionRequest) validate() error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

// satangFromTHB converts a client-supplied THB amount to integer satang
// using bank-style rounding so 0.07 doesn't truncate to 6 satang.
func satangFromTHB(thb float64) int64 {
	return int64(math.Round(thb * 100))
}

func (h *Handler) CreateOption(w http.ResponseWriter, r *http.Request) {
	groupID, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	var req optionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	o, err := h.svc.CreateOption(r.Context(), groupID, req.Name, satangFromTHB(req.PriceDelta), req.SortOrder)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to create option", err)
		return
	}
	response.Created(w, r, toOptionResponse(o))
}

func (h *Handler) UpdateOption(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	var req optionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	o, err := h.svc.UpdateOption(r.Context(), id, req.Name, satangFromTHB(req.PriceDelta), req.SortOrder)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to update option", err)
		return
	}
	response.OK(w, r, toOptionResponse(o))
}

func (h *Handler) DeleteOption(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	if err := h.svc.DeleteOption(r.Context(), id); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to delete option", err)
		return
	}
	response.NoContent(w, r)
}

type setMenuGroupsRequest struct {
	GroupIDs []int32 `json:"group_ids"`
}

func (h *Handler) SetMenuGroups(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	var req setMenuGroupsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := h.svc.SetMenuGroups(r.Context(), id, req.GroupIDs); err != nil {
		if errors.Is(err, service.ErrUnknownGroup) {
			response.Error(w, r, http.StatusBadRequest, "unknown option group", err)
			return
		}
		response.Error(w, r, http.StatusInternalServerError, "failed to set menu groups", err)
		return
	}
	response.NoContent(w, r)
}
