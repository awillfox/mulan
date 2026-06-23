package http

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"mulan/internal/response"
	"mulan/internal/settings/service"
)

// maxLogoBytes caps logo upload size so a misbehaving client can't fill the
// settings row with a 50MB blob. 2 MiB comfortably holds a high-res PNG.
const maxLogoBytes = 2 * 1024 * 1024

type Handler struct {
	svc *service.SettingsService
}

func NewHandler(svc *service.SettingsService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Get("/", h.Get)
	r.Patch("/", h.Update)
	r.Get("/logo", h.GetLogo)
	r.Put("/logo", h.PutLogo)
	r.Delete("/logo", h.DeleteLogo)
}

// ServeLogo is also wired at /elements/logo.png so existing consumers (the
// agent's receipt printer) keep working without code changes.
func (h *Handler) ServeLogo(w http.ResponseWriter, r *http.Request) {
	h.GetLogo(w, r)
}

func (h *Handler) GetLogo(w http.ResponseWriter, r *http.Request) {
	logo := h.svc.GetLogo()
	if len(logo.Bytes) == 0 {
		// 404 lets the FileServer fall through to elements/logo.png on disk
		// when chi is configured that way; here we just signal "no logo".
		http.NotFound(w, r)
		return
	}
	mime := logo.MIME
	if mime == "" {
		mime = "image/png"
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Content-Length", strconv.Itoa(len(logo.Bytes)))
	w.Header().Set("Cache-Control", "public, max-age=60")
	if !logo.UpdatedAt.IsZero() {
		w.Header().Set("Last-Modified", logo.UpdatedAt.UTC().Format(http.TimeFormat))
	}
	_, _ = w.Write(logo.Bytes)
}

// PutLogo accepts multipart/form-data with field "file" or a raw body. We
// detect by content-type so the manager UI (multipart) and curl tests (raw)
// both work.
func (h *Handler) PutLogo(w http.ResponseWriter, r *http.Request) {
	var (
		body []byte
		mime string
		err  error
	)
	ct := r.Header.Get("Content-Type")
	if len(ct) >= 19 && ct[:19] == "multipart/form-data" {
		// Cap body size at the same limit to stop slowloris-style uploads.
		r.Body = http.MaxBytesReader(w, r.Body, maxLogoBytes+1024)
		if perr := r.ParseMultipartForm(maxLogoBytes + 1024); perr != nil {
			response.Error(w, r, http.StatusBadRequest, "invalid multipart body", perr)
			return
		}
		f, hdr, ferr := r.FormFile("file")
		if ferr != nil {
			response.Error(w, r, http.StatusBadRequest, "file field required", ferr)
			return
		}
		defer f.Close()
		body, err = io.ReadAll(io.LimitReader(f, maxLogoBytes+1))
		if err != nil {
			response.Error(w, r, http.StatusBadRequest, "failed to read file", err)
			return
		}
		mime = hdr.Header.Get("Content-Type")
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, maxLogoBytes+1)
		body, err = io.ReadAll(r.Body)
		if err != nil {
			response.Error(w, r, http.StatusBadRequest, "failed to read body", err)
			return
		}
		mime = ct
	}

	if len(body) == 0 {
		response.Error(w, r, http.StatusBadRequest, "empty body", nil)
		return
	}
	if len(body) > maxLogoBytes {
		response.Error(w, r, http.StatusRequestEntityTooLarge, "logo exceeds 2 MiB", nil)
		return
	}
	// Sniff if no MIME provided — keeps the field accurate for serving.
	if mime == "" {
		mime = http.DetectContentType(body)
	}
	if !isImageMIME(mime) {
		response.Error(w, r, http.StatusBadRequest, "logo must be an image", errors.New(mime))
		return
	}

	if err := h.svc.SetLogo(r.Context(), body, mime); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to save logo", err)
		return
	}
	response.OK(w, r, map[string]any{
		"size":       len(body),
		"mime":       mime,
		"updated_at": h.svc.GetLogo().UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (h *Handler) DeleteLogo(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.ClearLogo(r.Context()); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to clear logo", err)
		return
	}
	response.NoContent(w, r)
}

func isImageMIME(m string) bool {
	switch m {
	case "image/png", "image/jpeg", "image/jpg", "image/gif", "image/webp", "image/svg+xml":
		return true
	}
	return false
}

type settingsResponse struct {
	ShopName      string  `json:"shop_name"`
	VATPercent    float64 `json:"vat_percent"`
	ReceiptFooter string  `json:"receipt_footer"`
	PointsPerBaht float64 `json:"points_per_baht"`
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	row := h.svc.Get()
	response.OK(w, r, settingsResponse{
		ShopName:      row.ShopName,
		VATPercent:    row.VatPercent,
		ReceiptFooter: row.ReceiptFooter,
		PointsPerBaht: row.PointsPerBaht,
	})
}

type updateRequest struct {
	ShopName      string  `json:"shop_name"`
	VATPercent    float64 `json:"vat_percent"`
	ReceiptFooter string  `json:"receipt_footer"`
	PointsPerBaht float64 `json:"points_per_baht"`
}

func (req updateRequest) validate() error {
	if req.ShopName == "" {
		return errors.New("shop_name is required")
	}
	if req.VATPercent < 0 || req.VATPercent > 100 {
		return errors.New("vat_percent must be between 0 and 100")
	}
	if len(req.ReceiptFooter) > 255 {
		return errors.New("receipt_footer must be 255 chars or fewer")
	}
	if req.PointsPerBaht < 0 {
		return errors.New("points_per_baht must be >= 0")
	}
	return nil
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if err := req.validate(); err != nil {
		response.Error(w, r, http.StatusBadRequest, err.Error(), err)
		return
	}

	row, err := h.svc.Update(r.Context(), req.ShopName, req.VATPercent, req.ReceiptFooter, req.PointsPerBaht)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to update settings", err)
		return
	}

	response.OK(w, r, settingsResponse{
		ShopName:      row.ShopName,
		VATPercent:    row.VatPercent,
		ReceiptFooter: row.ReceiptFooter,
		PointsPerBaht: row.PointsPerBaht,
	})
}
