package http

import (
	"encoding/json"
	"net/http"
	"strconv"

	"fmt"

	"github.com/Rhymond/go-money"
	"github.com/go-chi/chi/v5"

	"mulan/internal/hub"
	"mulan/internal/menu/service"
	"mulan/sqlc"
)

type MenuHandler struct {
	service *service.MenuService
	hub     *hub.Hub
}

type menuResponse struct {
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	Price      float64 `json:"price"`
	CategoryID *int32  `json:"category_id,omitempty"`
	VfdName    *string `json:"vfd_name,omitempty"`
	Active     bool    `json:"active"`
}

func NewMenuHandler(s *service.MenuService, h *hub.Hub) *MenuHandler {
	return &MenuHandler{service: s, hub: h}
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
		ID:         int(m.ID),
		Name:       m.Name,
		Price:      money.New(m.Price, money.THB).AsMajorUnits(),
		CategoryID: catID,
		VfdName:    vfdName,
		Active:     m.Active,
	}
}

func (h *MenuHandler) List(w http.ResponseWriter, r *http.Request) {
	menus, err := h.service.List(r.Context())
	if err != nil {
		http.Error(w, "failed to list menus", http.StatusInternalServerError)
		return
	}
	resp := make([]menuResponse, len(menus))
	for i, m := range menus {
		resp[i] = toMenuResponse(m)
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type menuRequest struct {
	Name       string  `json:"name"`
	Price      float64 `json:"price"` // THB
	CategoryID *int32  `json:"category_id"`
	VfdName    *string `json:"vfd_name"`
}

func (h *MenuHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req menuRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	m, err := h.service.Create(r.Context(), req.Name, int64(req.Price*100), req.CategoryID, req.VfdName)
	if err != nil {
		http.Error(w, "failed to create menu", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(toMenuResponse(m))
}

func (h *MenuHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	var req menuRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	m, err := h.service.Update(r.Context(), int32(id), req.Name, int64(req.Price*100), req.CategoryID, req.VfdName)
	if err != nil {
		http.Error(w, "failed to update menu", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toMenuResponse(m))
}

func (h *MenuHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	m, err := h.service.Toggle(r.Context(), int32(id))
	if err != nil {
		http.Error(w, "failed to toggle menu", http.StatusInternalServerError)
		return
	}
	resp := toMenuResponse(m)
	h.hub.Broadcast("menu_status", fmt.Sprintf(`{"id":%d,"active":%v,"name":%q}`, resp.ID, resp.Active, resp.Name))
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *MenuHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	if err := h.service.Delete(r.Context(), int32(id)); err != nil {
		http.Error(w, "failed to delete menu", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
