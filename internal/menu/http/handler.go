package http

import (
	"encoding/json"
	"net/http"

	"github.com/Rhymond/go-money"

	"mulan/internal/menu/service"
)

type MenuHandler struct {
	service *service.MenuService
}

type menuResponse struct {
	ID    int     `json:"id"`
	Name  string  `json:"name"`
	Price float64 `json:"price"`
	CategoryID *int `json:"category_id,omitempty"`
}

func NewMenuHandler(s *service.MenuService) *MenuHandler {
	return &MenuHandler{service: s}
}

func (h *MenuHandler) List(w http.ResponseWriter, r *http.Request) {
	menus := h.service.List()

	resp := make([]menuResponse, len(menus))
	for i, m := range menus {
		resp[i] = menuResponse{
			ID:    m.ID,
			Name:  m.Name,
			Price: money.New(m.Price, money.THB).AsMajorUnits(),
			CategoryID: m.CategoryID,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
