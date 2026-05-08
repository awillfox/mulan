package service

import (
	"context"
	"crypto/rand"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"mulan/internal/order/domain"
	optiongroupservice "mulan/internal/optiongroup/service"
	settingsservice "mulan/internal/settings/service"
	"mulan/sqlc"
)

const codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

type OrderService struct {
	q         *sqlc.Queries
	settings  *settingsservice.SettingsService
	optionsvc *optiongroupservice.Service
}

func NewOrderService(q *sqlc.Queries, settings *settingsservice.SettingsService, optionsvc *optiongroupservice.Service) *OrderService {
	return &OrderService{q: q, settings: settings, optionsvc: optionsvc}
}

func (s *OrderService) Create(ctx context.Context) (string, error) {
	code, err := generateCode()
	if err != nil {
		return "", fmt.Errorf("generate code: %w", err)
	}
	row, err := s.q.CreateOrder(ctx, code)
	if err != nil {
		return "", fmt.Errorf("create order: %w", err)
	}
	return row.Code, nil
}

type CheckoutItemInput struct {
	MenuID    int32
	Name      string
	Price     int64 // satang, base unit price
	Qty       int32
	OptionIDs []int32
}

func (s *OrderService) Checkout(ctx context.Context, code string, items []CheckoutItemInput) (*domain.CheckoutResult, error) {
	order, err := s.q.GetOrderByCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}

	// Resolve all distinct option IDs once
	idSet := make(map[int32]struct{})
	for _, it := range items {
		for _, oid := range it.OptionIDs {
			idSet[oid] = struct{}{}
		}
	}
	ids := make([]int32, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	resolved, err := s.optionsvc.ResolveOptions(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("resolve options: %w", err)
	}
	optByID := make(map[int32]sqlc.Option, len(resolved))
	for _, o := range resolved {
		optByID[o.ID] = o
	}

	resultItems := make([]domain.CheckoutResultItem, 0, len(items))
	var subtotalSatang int64

	for _, it := range items {
		opts := make([]domain.SelectedOption, 0, len(it.OptionIDs))
		var deltaSum int64
		for _, oid := range it.OptionIDs {
			o, ok := optByID[oid]
			if !ok {
				return nil, fmt.Errorf("option %d not found", oid)
			}
			opts = append(opts, domain.SelectedOption{ID: o.ID, Name: o.Name, PriceDelta: o.PriceDelta})
			deltaSum += o.PriceDelta
		}
		unitPrice := it.Price + deltaSum
		lineTotal := unitPrice * int64(it.Qty)
		subtotalSatang += lineTotal

		itemID, err := s.q.CreateOrderItem(ctx, sqlc.CreateOrderItemParams{
			OrderID: order.ID,
			MenuID:  pgtype.Int4{Int32: it.MenuID, Valid: it.MenuID > 0},
			Name:    it.Name,
			Price:   it.Price,
			Qty:     it.Qty,
		})
		if err != nil {
			return nil, fmt.Errorf("insert order item: %w", err)
		}
		for _, o := range opts {
			if err := s.q.CreateOrderItemOption(ctx, sqlc.CreateOrderItemOptionParams{
				OrderItemID: itemID,
				OptionID:    pgtype.Int4{Int32: o.ID, Valid: true},
				Name:        o.Name,
				PriceDelta:  o.PriceDelta,
			}); err != nil {
				return nil, fmt.Errorf("insert order item option: %w", err)
			}
		}

		resultItems = append(resultItems, domain.CheckoutResultItem{
			Name: it.Name, Price: it.Price, Qty: it.Qty, Options: opts,
		})
	}

	if err := s.q.PayOrder(ctx, code); err != nil {
		return nil, fmt.Errorf("pay order: %w", err)
	}

	cfg := s.settings.Get()
	subtotal := float64(subtotalSatang) / 100
	vat := subtotal * cfg.VatPercent / 100
	total := subtotal + vat

	return &domain.CheckoutResult{
		Code:       code,
		Subtotal:   subtotal,
		VAT:        vat,
		VATPercent: cfg.VatPercent,
		ShopName:   cfg.ShopName,
		Total:      total,
		Items:      resultItems,
	}, nil
}

func generateCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := make([]byte, 8)
	for i, v := range b {
		code[i] = codeChars[int(v)%len(codeChars)]
	}
	return string(code), nil
}
