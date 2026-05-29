package http

import (
	"net/http"
	"strconv"

	"mulan/internal/guestwifi/service"
	"mulan/internal/response"

	"github.com/go-chi/chi/v5"
)

type Handler struct {
	svc *service.Service
}

func New(svc *service.Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Get("/order/{id}", h.getForOrder)
	return r
}

func (h *Handler) getForOrder(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid order id", err)
		return
	}
	username := h.svc.GetUsernameForOrder(r.Context(), int32(id))
	response.OK(w, r, map[string]string{"username": username})
}
