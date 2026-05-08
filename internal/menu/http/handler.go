package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Rhymond/go-money"

	"mulan/internal/httpx"
	"mulan/internal/hub"
	"mulan/internal/menu/service"
	optiongroupservice "mulan/internal/optiongroup/service"
	"mulan/internal/response"
	"mulan/sqlc"
)

type MenuHandler struct {
	svc        *service.MenuService
	optionsvc  *optiongroupservice.Service
	hub        *hub.Hub
}

type menuOptionResponse struct {
	ID         int32   `json:"id"`
	Name       string  `json:"name"`
	PriceDelta float64 `json:"price_delta"`
}

type menuOptionGroupResponse struct {
	ID            int32                `json:"id"`
	Name          string               `json:"name"`
	SelectionMode string               `json:"selection_mode"`
	Options       []menuOptionResponse `json:"options"`
}

type menuResponse struct {
	ID           int                       `json:"id"`
	Name         string                    `json:"name"`
	Price        float64                   `json:"price"`
	CategoryID   *int32                    `json:"category_id,omitempty"`
	VfdName      *string                   `json:"vfd_name,omitempty"`
	Active       bool                      `json:"active"`
	OptionGroups []menuOptionGroupResponse `json:"option_groups"`
}

func NewMenuHandler(s *service.MenuService, optionsvc *optiongroupservice.Service, h *hub.Hub) *MenuHandler {
	return &MenuHandler{svc: s, optionsvc: optionsvc, hub: h}
}

func toMenuResponse(m sqlc.Menu) menuResponse {
	var catID *int32
	if m.CategoryID.Valid {
		catID = &m.CategoryID.Int32
	}
	var vfdName *string
	if m.VfdName.Valid {
		vfdName = &m.VfdName.String
	}
	return menuResponse{
		ID:           int(m.ID),
		Name:         m.Name,
		Price:        money.New(m.Price, money.THB).AsMajorUnits(),
		CategoryID:   catID,
		VfdName:      vfdName,
		Active:       m.Active,
		OptionGroups: []menuOptionGroupResponse{},
	}
}

func toMenuOptionGroups(groups []optiongroupservice.GroupWithOptions) []menuOptionGroupResponse {
	out := make([]menuOptionGroupResponse, len(groups))
	for i, g := range groups {
		opts := make([]menuOptionResponse, len(g.Options))
		for j, o := range g.Options {
			opts[j] = menuOptionResponse{
				ID:         o.ID,
				Name:       o.Name,
				PriceDelta: money.New(o.PriceDelta, money.THB).AsMajorUnits(),
			}
		}
		out[i] = menuOptionGroupResponse{
			ID:            g.Group.ID,
			Name:          g.Group.Name,
			SelectionMode: g.Group.SelectionMode,
			Options:       opts,
		}
	}
	return out
}

func (h *MenuHandler) List(w http.ResponseWriter, r *http.Request) {
	menus, err := h.svc.List(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("list menus: %w", err))
		return
	}
	ids := make([]int32, len(menus))
	for i, m := range menus {
		ids[i] = m.ID
	}
	groupsByMenu, err := h.optionsvc.GroupsForMenus(r.Context(), ids)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("list menu options: %w", err))
		return
	}
	out := make([]menuResponse, len(menus))
	for i, m := range menus {
		mr := toMenuResponse(m)
		mr.OptionGroups = toMenuOptionGroups(groupsByMenu[m.ID])
		out[i] = mr
	}
	response.OK(w, r, out)
}

type menuRequest struct {
	Name       string  `json:"name"`
	Price      float64 `json:"price"`
	CategoryID *int32  `json:"category_id"`
	VfdName    *string `json:"vfd_name"`
}

func (req menuRequest) validate() error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func (h *MenuHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req menuRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, errors.New("invalid body"))
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err)
		return
	}
	m, err := h.svc.Create(r.Context(), req.Name, int64(req.Price*100), req.CategoryID, req.VfdName)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("create menu: %w", err))
		return
	}
	response.Created(w, r, toMenuResponse(m))
}

func (h *MenuHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err)
		return
	}
	var req menuRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, errors.New("invalid body"))
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err)
		return
	}
	m, err := h.svc.Update(r.Context(), id, req.Name, int64(req.Price*100), req.CategoryID, req.VfdName)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("update menu: %w", err))
		return
	}
	response.OK(w, r, toMenuResponse(m))
}

func (h *MenuHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err)
		return
	}
	m, err := h.svc.Toggle(r.Context(), id)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("toggle menu: %w", err))
		return
	}
	out := toMenuResponse(m)
	h.hub.Broadcast("menu_status", fmt.Sprintf(`{"id":%d,"active":%v,"name":%q}`, out.ID, out.Active, out.Name))
	response.OK(w, r, out)
}

func (h *MenuHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("delete menu: %w", err))
		return
	}
	response.NoContent(w, r)
}
