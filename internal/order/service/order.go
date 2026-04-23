package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"mulan/internal/order/domain"
	settingsservice "mulan/internal/settings/service"
	"mulan/sqlc"
)

const codeChars = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

type OrderService struct {
	q        *sqlc.Queries
	settings *settingsservice.SettingsService
}

func NewOrderService(q *sqlc.Queries, settings *settingsservice.SettingsService) *OrderService {
	return &OrderService{q: q, settings: settings}
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

func (s *OrderService) Checkout(ctx context.Context, code string, items []domain.OrderItem) (*domain.CheckoutResult, error) {
	order, err := s.q.GetOrderByCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}

	for _, it := range items {
		menuID := pgtype.Int4{Int32: it.MenuID, Valid: it.MenuID > 0}
		if err := s.q.CreateOrderItem(ctx, sqlc.CreateOrderItemParams{
			OrderID: order.ID,
			MenuID:  menuID,
			Name:    it.Name,
			Price:   it.Price,
			Qty:     it.Qty,
		}); err != nil {
			return nil, fmt.Errorf("insert order item: %w", err)
		}
	}

	subtotalSatang, err := s.q.SumOrderItems(ctx, order.ID)
	if err != nil {
		return nil, fmt.Errorf("sum items: %w", err)
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
		Items:      items,
	}, nil
}

type TodaySummary struct {
	Sales  float64 `json:"sales"`
	Orders int64   `json:"orders"`
}

func (s *OrderService) TodaySummary(ctx context.Context) (*TodaySummary, error) {
	sales, err := s.q.SumTodaySales(ctx)
	if err != nil {
		return nil, fmt.Errorf("sum today sales: %w", err)
	}
	orders, err := s.q.CountTodayOrders(ctx)
	if err != nil {
		return nil, fmt.Errorf("count today orders: %w", err)
	}
	return &TodaySummary{
		Sales:  float64(sales) / 100,
		Orders: orders,
	}, nil
}

type TopMenuItem struct {
	Name    string  `json:"name"`
	QtySold int64   `json:"qty_sold"`
	Revenue float64 `json:"revenue"`
}

func (s *OrderService) TopMenus(ctx context.Context, from, to time.Time) ([]TopMenuItem, error) {
	rows, err := s.q.TopMenusBySales(ctx, sqlc.TopMenusBySalesParams{
		Column1: pgtype.Timestamptz{Time: from, Valid: true},
		Column2: pgtype.Timestamptz{Time: to, Valid: true},
	})
	if err != nil {
		return nil, fmt.Errorf("top menus: %w", err)
	}
	result := make([]TopMenuItem, len(rows))
	for i, r := range rows {
		result[i] = TopMenuItem{
			Name:    r.Name,
			QtySold: r.QtySold,
			Revenue: float64(r.Revenue) / 100,
		}
	}
	return result, nil
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
