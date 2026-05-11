package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"mulan/internal/httpx"
	"mulan/internal/menucategory/service"
	"mulan/internal/response"
)

type CategoryHandler struct {
	svc *service.CategoryService
}

func NewCategoryHandler(s *service.CategoryService) *CategoryHandler {
	return &CategoryHandler{svc: s}
}

func (h *CategoryHandler) List(w http.ResponseWriter, r *http.Request) {
	categories, err := h.svc.List(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to list categories", err)
		return
	}
	response.OK(w, r, categories)
}

type categoryRequest struct {
	Name string `json:"name"`
}

func (req categoryRequest) validate() error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func (h *CategoryHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	c, err := h.svc.Create(r.Context(), req.Name)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to create category", err)
		return
	}
	response.Created(w, r, c)
}

func (h *CategoryHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	var req categoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	c, err := h.svc.Update(r.Context(), id, req.Name)
	if err != nil {
		response.Error(w, r, http.StatusNotFound, "category not found", err)
		return
	}
	response.OK(w, r, c)
}

func (h *CategoryHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.URLParamInt32(r, "id")
	if err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		response.Error(w, r, http.StatusNotFound, "category not found", err)
		return
	}
	response.NoContent(w, r)
}
