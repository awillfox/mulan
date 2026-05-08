package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/Rhymond/go-money"
	"github.com/go-chi/chi/v5"

	"mulan/internal/order/service"
	"mulan/internal/response"
)

type Handler struct {
	svc *service.OrderService
}

func NewHandler(svc *service.OrderService) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/", h.create)
	r.Post("/{code}/checkout", h.checkout)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	code, err := h.svc.Create(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("create order: %w", err))
		return
	}
	response.Created(w, r, map[string]string{"code": code})
}

type checkoutItemRequest struct {
	MenuID    int32   `json:"menu_id"`
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Qty       int32   `json:"qty"`
	OptionIDs []int32 `json:"option_ids"`
}

type checkoutRequest struct {
	Items []checkoutItemRequest `json:"items"`
}

type checkoutOptionResponse struct {
	Name       string  `json:"name"`
	PriceDelta float64 `json:"price_delta"`
}

type checkoutItemResponse struct {
	Name    string                   `json:"name"`
	Price   float64                  `json:"price"`
	Qty     int32                    `json:"qty"`
	Options []checkoutOptionResponse `json:"options"`
}

type checkoutResponse struct {
	Code       string                 `json:"code"`
	Subtotal   float64                `json:"subtotal"`
	VAT        float64                `json:"vat"`
	VATPercent float64                `json:"vat_percent"`
	ShopName   string                 `json:"shop_name"`
	Total      float64                `json:"total"`
	Items      []checkoutItemResponse `json:"items"`
}

func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	var req checkoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, errors.New("invalid body"))
		return
	}
	if len(req.Items) == 0 {
		response.Error(w, r, http.StatusBadRequest, errors.New("no items"))
		return
	}

	items := make([]service.CheckoutItemInput, len(req.Items))
	for i, it := range req.Items {
		items[i] = service.CheckoutItemInput{
			MenuID:    it.MenuID,
			Name:      it.Name,
			Price:     int64(it.Price * 100),
			Qty:       it.Qty,
			OptionIDs: it.OptionIDs,
		}
	}

	result, err := h.svc.Checkout(r.Context(), code, items)
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, fmt.Errorf("checkout: %w", err))
		return
	}

	respItems := make([]checkoutItemResponse, len(result.Items))
	for i, it := range result.Items {
		opts := make([]checkoutOptionResponse, len(it.Options))
		for j, o := range it.Options {
			opts[j] = checkoutOptionResponse{
				Name:       o.Name,
				PriceDelta: money.New(o.PriceDelta, money.THB).AsMajorUnits(),
			}
		}
		respItems[i] = checkoutItemResponse{
			Name:    it.Name,
			Price:   money.New(it.Price, money.THB).AsMajorUnits(),
			Qty:     it.Qty,
			Options: opts,
		}
	}

	response.OK(w, r, checkoutResponse{
		Code:       result.Code,
		Subtotal:   result.Subtotal,
		VAT:        result.VAT,
		VATPercent: result.VATPercent,
		ShopName:   result.ShopName,
		Total:      result.Total,
		Items:      respItems,
	})
}
