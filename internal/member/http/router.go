package http

import "github.com/go-chi/chi/v5"

func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.List)
	r.Post("/", h.Create)
	r.Get("/lookup", h.Lookup)
	r.Get("/{id}/orders", h.Orders)
	r.Patch("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
}
