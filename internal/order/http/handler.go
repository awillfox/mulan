package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Rhymond/go-money"
	"github.com/go-chi/chi/v5"

	"mulan/internal/order/service"
	"mulan/internal/response"
)

// WifiService is the subset of guestwifi.Service used by this handler.
type WifiService interface {
	AssignToOrder(ctx context.Context, orderID int32) (string, error)
	EnableForOrder(ctx context.Context, orderID int32) error
	GetUsernameForOrder(ctx context.Context, orderID int32) string
}

type heldOrderResponse struct {
	Code      string          `json:"code"`
	CreatedAt string          `json:"created_at,omitempty"`
	HeldAt    string          `json:"held_at,omitempty"`
	HeldLabel *string         `json:"held_label,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

func toHeldResponse(h service.HeldOrder) heldOrderResponse {
	out := heldOrderResponse{
		Code:      h.Code,
		HeldLabel: h.HeldLabel,
		Payload:   json.RawMessage(h.Payload),
	}
	if h.CreatedAt.Valid {
		out.CreatedAt = h.CreatedAt.Time.UTC().Format("2006-01-02T15:04:05Z")
	}
	if h.HeldAt.Valid {
		out.HeldAt = h.HeldAt.Time.UTC().Format("2006-01-02T15:04:05Z")
	}
	if len(out.Payload) == 0 {
		out.Payload = json.RawMessage(`{}`)
	}
	return out
}

type Handler struct {
	svc  *service.OrderService
	wifi WifiService // nil when WiFi feature is disabled
}

func NewHandler(svc *service.OrderService, wifi WifiService) *Handler {
	return &Handler{svc: svc, wifi: wifi}
}

func (h *Handler) Routes(r chi.Router) {
	r.Post("/", h.create)
	r.Post("/{code}/checkout", h.checkout)
	r.Get("/held", h.listHeld)
	r.Put("/{code}/hold", h.hold)
	r.Post("/{code}/resume", h.resume)
	r.Delete("/{code}/hold", h.discardHeld)
}

type holdRequest struct {
	Label   *string         `json:"label,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

func (h *Handler) hold(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	var req holdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, r, http.StatusBadRequest, "invalid body", err)
		return
	}
	if len(req.Payload) == 0 {
		response.Error(w, r, http.StatusBadRequest, "payload required", nil)
		return
	}
	held, err := h.svc.Hold(r.Context(), code, req.Label, req.Payload)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrOrderNotFound):
			response.Error(w, r, http.StatusNotFound, "order not found", err)
		case errors.Is(err, service.ErrAlreadyPaid):
			response.Error(w, r, http.StatusConflict, "order already paid", err)
		default:
			response.Error(w, r, http.StatusInternalServerError, "failed to hold order", err)
		}
		return
	}
	response.OK(w, r, toHeldResponse(held))
}

func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	payload, err := h.svc.Resume(r.Context(), code)
	if err != nil {
		if errors.Is(err, service.ErrNotHeld) {
			response.Error(w, r, http.StatusNotFound, "order not held", err)
			return
		}
		response.Error(w, r, http.StatusInternalServerError, "failed to resume order", err)
		return
	}
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	response.OK(w, r, map[string]any{
		"code":    code,
		"payload": json.RawMessage(payload),
	})
}

func (h *Handler) discardHeld(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if err := h.svc.DiscardHeld(r.Context(), code); err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to discard held order", err)
		return
	}
	response.NoContent(w, r)
}

func (h *Handler) listHeld(w http.ResponseWriter, r *http.Request) {
	list, err := h.svc.ListHeld(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to list held orders", err)
		return
	}
	out := make([]heldOrderResponse, len(list))
	for i, h := range list {
		out[i] = toHeldResponse(h)
	}
	response.OK(w, r, out)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	order, err := h.svc.Create(r.Context())
	if err != nil {
		response.Error(w, r, http.StatusInternalServerError, "failed to create order", err)
		return
	}
	out := map[string]any{"code": order.Code}
	if h.wifi != nil {
		username, _ := h.wifi.AssignToOrder(r.Context(), order.ID)
		out["wifi_username"] = username
	}
	response.Created(w, r, out)
}

type checkoutItemRequest struct {
	MenuID      int32   `json:"menu_id"`
	Qty         int32   `json:"qty"`
	OptionIDs   []int32 `json:"option_ids"`
	DiscountIDs []int32 `json:"discount_ids"`
}

type checkoutRequest struct {
	Items         []checkoutItemRequest `json:"items"`
	DiscountIDs   []int32               `json:"discount_ids"` // whole-order discounts
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

type checkoutDiscountResponse struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Amount    float64 `json:"amount"`
	IsSubsidy bool    `json:"is_subsidy"`
}

type checkoutResponse struct {
	Code          string                     `json:"code"`
	Subtotal      float64                    `json:"subtotal"`
	Discount      float64                    `json:"discount"`
	Subsidy       float64                    `json:"subsidy"`
	VAT           float64                    `json:"vat"`
	VATPercent    float64                    `json:"vat_percent"`
	ShopName      string                     `json:"shop_name"`
	ReceiptFooter string                     `json:"receipt_footer"`
	Total         float64                    `json:"total"`
	Items         []checkoutItemResponse     `json:"items"`
	Discounts     []checkoutDiscountResponse `json:"discounts"`
	HasMember     bool                       `json:"has_member"`
	MemberName    string                     `json:"member_name,omitempty"`
	MemberPhone   string                     `json:"member_phone,omitempty"`
	PointsEarned  int64                      `json:"points_earned"`
	PointsBalance int64                      `json:"points_balance"`
	WifiUsername  string                     `json:"wifi_username,omitempty"`
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
			MenuID:      it.MenuID,
			Qty:         it.Qty,
			OptionIDs:   it.OptionIDs,
			DiscountIDs: it.DiscountIDs,
		}
	}

	result, err := h.svc.Checkout(r.Context(), code, items, req.DiscountIDs, service.CustomerInput{
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

	respDiscounts := make([]checkoutDiscountResponse, len(result.Discounts))
	for i, d := range result.Discounts {
		respDiscounts[i] = checkoutDiscountResponse{
			Name:      d.Name,
			Type:      d.Type,
			Amount:    money.New(d.Amount, money.THB).AsMajorUnits(),
			IsSubsidy: d.IsSubsidy,
		}
	}

	var wifiUsername string
	if h.wifi != nil {
		if err := h.wifi.EnableForOrder(r.Context(), result.OrderID); err != nil {
			// log but don't fail — payment already committed
			_ = err
		}
		wifiUsername = h.wifi.GetUsernameForOrder(r.Context(), result.OrderID)
	}

	response.OK(w, r, checkoutResponse{
		Code:          result.Code,
		Subtotal:      result.Subtotal,
		Discount:      result.Discount,
		Subsidy:       result.Subsidy,
		VAT:           result.VAT,
		VATPercent:    result.VATPercent,
		ShopName:      result.ShopName,
		ReceiptFooter: result.ReceiptFooter,
		Total:         result.Total,
		Items:         respItems,
		Discounts:     respDiscounts,
		HasMember:     result.HasMember,
		MemberName:    result.MemberName,
		MemberPhone:   result.MemberPhone,
		PointsEarned:  result.PointsEarned,
		PointsBalance: result.PointsBalance,
		WifiUsername:  wifiUsername,
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
	case errors.Is(err, service.ErrUnknownDiscount):
		return http.StatusBadRequest, "unknown discount"
	case errors.Is(err, service.ErrDiscountInactive):
		return http.StatusBadRequest, "discount is not available"
	default:
		return http.StatusInternalServerError, "checkout failed"
	}
}
