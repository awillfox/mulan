package service

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"mulan/internal/report/domain"
	"mulan/sqlc"
)

type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service {
	return &Service{q: q}
}

type ListParams struct {
	Status string // "" = all
	From   time.Time
	To     time.Time
	Limit  int32
	Offset int32
}

func (s *Service) ListOrders(ctx context.Context, p ListParams) (domain.Page, error) {
	fromAt := pgtype.Timestamptz{Time: p.From, Valid: true}
	toAt := pgtype.Timestamptz{Time: p.To, Valid: true}

	total, err := s.q.CountOrdersPage(ctx, sqlc.CountOrdersPageParams{
		Status: p.Status, FromAt: fromAt, ToAt: toAt,
	})
	if err != nil {
		return domain.Page{}, fmt.Errorf("count orders: %w", err)
	}

	rows, err := s.q.ListOrdersPage(ctx, sqlc.ListOrdersPageParams{
		Status: p.Status, FromAt: fromAt, ToAt: toAt, Lim: p.Limit, Off: p.Offset,
	})
	if err != nil {
		return domain.Page{}, fmt.Errorf("list orders: %w", err)
	}

	orderRows := make([]OrderRow, len(rows))
	ids := make([]int32, len(rows))
	for i, r := range rows {
		orderRows[i] = OrderRow{
			ID:           r.ID,
			Code:         r.Code,
			Status:       r.Status,
			CreatedAt:    r.CreatedAt.Time,
			PaidAt:       r.PaidAt,
			MemberName:   r.MemberName,
			MemberPhone:  r.MemberPhone,
			PointsEarned: r.PointsEarned,
		}
		ids[i] = r.ID
	}

	if len(ids) == 0 {
		return domain.Page{Orders: []domain.Order{}, Total: total}, nil
	}

	itemRows, err := s.q.ListOrderItemsByOrderIDs(ctx, ids)
	if err != nil {
		return domain.Page{}, fmt.Errorf("list items: %w", err)
	}
	items := make([]ItemRow, len(itemRows))
	itemIDs := make([]int32, len(itemRows))
	for i, it := range itemRows {
		items[i] = ItemRow{
			ID: it.ID, OrderID: it.OrderID, Name: it.Name,
			BaseOptionName: it.BaseOptionName, Price: it.Price, Qty: it.Qty,
		}
		itemIDs[i] = it.ID
	}

	var options []OptionRow
	if len(itemIDs) > 0 {
		optRows, err := s.q.ListOrderItemOptionsByItemIDs(ctx, itemIDs)
		if err != nil {
			return domain.Page{}, fmt.Errorf("list options: %w", err)
		}
		options = make([]OptionRow, len(optRows))
		for i, op := range optRows {
			options[i] = OptionRow{OrderItemID: op.OrderItemID, Name: op.Name, PriceDelta: op.PriceDelta}
		}
	}

	discRows, err := s.q.ListOrderDiscountsByOrderIDs(ctx, ids)
	if err != nil {
		return domain.Page{}, fmt.Errorf("list discounts: %w", err)
	}
	discounts := make([]DiscountRow, len(discRows))
	for i, d := range discRows {
		discounts[i] = DiscountRow{
			OrderID: d.OrderID, Name: d.Name, DiscountType: d.DiscountType,
			Amount: d.Amount, IsSubsidy: d.IsSubsidy,
		}
	}

	return domain.Page{Orders: assemble(orderRows, items, options, discounts), Total: total}, nil
}
