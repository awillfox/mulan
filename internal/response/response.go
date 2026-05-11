package response

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/go-chi/render"
)

type HTTPResponse struct {
	Data    any    `json:"data"`
	Message string `json:"-"`
}

func (h HTTPResponse) MarshalJSON() ([]byte, error) {
	out := struct {
		Data  any    `json:"data"`
		Error string `json:"error,omitempty"`
	}{Data: h.Data, Error: h.Message}
	return json.Marshal(out)
}

func OK(w http.ResponseWriter, r *http.Request, data any) {
	render.JSON(w, r, HTTPResponse{Data: data})
}

func Created(w http.ResponseWriter, r *http.Request, data any) {
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, HTTPResponse{Data: data})
}

func NoContent(w http.ResponseWriter, r *http.Request) {
	render.NoContent(w, r)
}

// Error responds with a sanitized client message and logs the underlying error
// server-side so internal details (SQL errors, wrapped strings) do not leak.
// `clientMsg` is what the user sees; `err` is logged with method+path context.
func Error(w http.ResponseWriter, r *http.Request, status int, clientMsg string, err error) {
	if err != nil {
		log.Printf("http %d %s %s: %v", status, r.Method, r.URL.Path, err)
	}
	if clientMsg == "" {
		clientMsg = http.StatusText(status)
	}
	render.Status(r, status)
	render.JSON(w, r, HTTPResponse{Message: clientMsg})
}
