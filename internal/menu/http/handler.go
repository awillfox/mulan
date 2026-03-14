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
	ID         int     `json:"id"`
	Name       string  `json:"name"`
	Price      float64 `json:"price"`
	CategoryID *int32  `json:"category_id,omitempty"`
	VfdName    *string `json:"vfd_name,omitempty"`
}

func NewMenuHandler(s *service.MenuService) *MenuHandler {
	return &MenuHandler{service: s}
}

func (h *MenuHandler) List(w http.ResponseWriter, r *http.Request) {
	menus, err := h.service.List(r.Context())
	if err != nil {
		http.Error(w, "failed to list menus", http.StatusInternalServerError)
		return
	}

	resp := make([]menuResponse, len(menus))
	for i, m := range menus {
		var catID *int32
		if m.CategoryID.Valid {
			catID = &m.CategoryID.Int32
		}
		var vfdName *string
		if m.VfdName.Valid {
			vfdName = &m.VfdName.String
		}
		resp[i] = menuResponse{
			ID:         int(m.ID),
			Name:       m.Name,
			Price:      money.New(m.Price, money.THB).AsMajorUnits(),
			CategoryID: catID,
			VfdName:    vfdName,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
