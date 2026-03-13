package http

import "github.com/go-chi/chi/v5"

func (h *MenuHandler) Routes(r chi.Router) {
	r.Get("/", h.List)
}
