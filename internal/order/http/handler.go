package http

import (
	"encoding/json"
	"errors"
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
		response.Error(w, r, http.StatusInternalServerError, "failed to create order", err)
		return
	}
	response.Created(w, r, map[string]string{"code": code})
}

type checkoutItemRequest struct {
	MenuID    int32   `json:"menu_id"`
	Qty       int32   `json:"qty"`
	OptionIDs []int32 `json:"option_ids"`
}

type checkoutRequest struct {
	Items         []checkoutItemRequest `json:"items"`
	CustomerPhone string                `json:"customer_phone"`
	CustomerName  string                `json:"customer_name"`
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
	Code          string                 `json:"code"`
	Subtotal      float64                `json:"subtotal"`
	VAT           float64                `json:"vat"`
	VATPercent    float64                `json:"vat_percent"`
	ShopName      string                 `json:"shop_name"`
	Total         float64                `json:"total"`
	Items         []checkoutItemResponse `json:"items"`
	HasMember     bool                   `json:"has_member"`
	MemberName    string                 `json:"member_name,omitempty"`
	MemberPhone   string                 `json:"member_phone,omitempty"`
	PointsEarned  int64                  `json:"points_earned"`
	PointsBalance int64                  `json:"points_balance"`
}

func (h *Handler) checkout(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")

	var req checkoutRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if len(req.Items) == 0 {
		response.Error(w, r, http.StatusBadRequest, "no items", nil)
		return
	}

	items := make([]service.CheckoutItemInput, len(req.Items))
	for i, it := range req.Items {
		items[i] = service.CheckoutItemInput{
			MenuID:    it.MenuID,
			Qty:       it.Qty,
			OptionIDs: it.OptionIDs,
		}
	}

	result, err := h.svc.Checkout(r.Context(), code, items, service.CustomerInput{
		Phone: req.CustomerPhone,
		Name:  req.CustomerName,
	})
	if err != nil {
		status, msg := classifyCheckoutError(err)
		response.Error(w, r, status, msg, err)
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
		Code:          result.Code,
		Subtotal:      result.Subtotal,
		VAT:           result.VAT,
		VATPercent:    result.VATPercent,
		ShopName:      result.ShopName,
		Total:         result.Total,
		Items:         respItems,
		HasMember:     result.HasMember,
		MemberName:    result.MemberName,
		MemberPhone:   result.MemberPhone,
		PointsEarned:  result.PointsEarned,
		PointsBalance: result.PointsBalance,
	})
}

// classifyCheckoutError maps service-level sentinel errors to client-facing
// HTTP status codes and messages. Unknown errors fall through to 500.
func classifyCheckoutError(err error) (int, string) {
	switch {
	case errors.Is(err, service.ErrAlreadyPaid):
		return http.StatusConflict, "order already paid"
	case errors.Is(err, service.ErrNoItems):
		return http.StatusBadRequest, "no items"
	case errors.Is(err, service.ErrUnknownMenu):
		return http.StatusBadRequest, "unknown menu"
	case errors.Is(err, service.ErrMenuInactive):
		return http.StatusBadRequest, "menu is not available"
	case errors.Is(err, service.ErrUnknownOption):
		return http.StatusBadRequest, "unknown option"
	case errors.Is(err, service.ErrInvalidOption):
		return http.StatusBadRequest, "option not valid for menu"
	default:
		return http.StatusInternalServerError, "checkout failed"
	}
}
