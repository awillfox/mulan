package web

import (
	"html/template"
	"net/http"
	"path/filepath"
)

type Handler struct {
	templateDir string
}

func NewHandler(templateDir string) *Handler {
	return &Handler{templateDir: templateDir}
}

func (h *Handler) POS(w http.ResponseWriter, r *http.Request) {
	h.render(w, "layouts/pos.html", "pos/index.html")
}

func (h *Handler) Manager(w http.ResponseWriter, r *http.Request) {
	h.render(w, "layouts/manager.html", "manager/index.html")
}

func (h *Handler) render(w http.ResponseWriter, files ...string) {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = filepath.Join(h.templateDir, f)
	}

	tmpl, err := template.ParseFiles(paths...)
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, files[0], nil); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
