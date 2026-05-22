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

func (h *Handler) Manager(w http.ResponseWriter, r *http.Request) {
	h.render(w, "layouts/manager.html", "manager/index.html")
}

func (h *Handler) Items(w http.ResponseWriter, r *http.Request) {
	h.render(w, "layouts/manager.html", "manager/items.html")
}

func (h *Handler) Settings(w http.ResponseWriter, r *http.Request) {
	h.render(w, "layouts/manager.html", "manager/settings.html")
}

func (h *Handler) OptionGroups(w http.ResponseWriter, r *http.Request) {
	h.render(w, "layouts/manager.html", "manager/option_groups.html")
}

func (h *Handler) Members(w http.ResponseWriter, r *http.Request) {
	h.render(w, "layouts/manager.html", "manager/members.html")
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
	if err := tmpl.ExecuteTemplate(w, filepath.Base(files[0]), nil); err != nil {
		http.Error(w, "render error", http.StatusInternalServerError)
	}
}
