package response

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/render"
)

type HTTPResponse struct {
	Data  any   `json:"data"`
	Error error `json:"-"`
}

func (h HTTPResponse) MarshalJSON() ([]byte, error) {
	out := struct {
		Data  any    `json:"data"`
		Error string `json:"error,omitempty"`
	}{Data: h.Data}
	if h.Error != nil {
		out.Error = h.Error.Error()
	}
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
	render.Status(r, http.StatusNoContent)
	render.NoContent(w, r)
}

func Error(w http.ResponseWriter, r *http.Request, status int, err error) {
	render.Status(r, status)
	render.JSON(w, r, HTTPResponse{Error: err})
}
