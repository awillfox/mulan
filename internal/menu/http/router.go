package http

import "github.com/go-chi/chi/v5"

func (h *MenuHandler) Routes(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Patch("/{id}", h.Update)
	r.Patch("/{id}/toggle", h.Toggle)
	r.Delete("/{id}", h.Delete)
}
