package service

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"mulan/internal/report/domain"
)

// Row structs decouple assemble() from sqlc-generated types so it is unit
// testable. Money fields are satang (int64); assemble converts to THB.
type OrderRow struct {
	ID           int32
	Code         string
	Status       string
	CreatedAt    time.Time
	PaidAt       pgtype.Timestamptz
	MemberName   string
	MemberPhone  string
	PointsEarned int64
}
type ItemRow struct {
	ID             int32
	OrderID        int32
	Name           string
	BaseOptionName string
	Price          int64
	Qty            int32
}
type OptionRow struct {
	OrderItemID int32
	Name        string
	PriceDelta  int64
}
type DiscountRow struct {
	OrderID      int32
	Name         string
	DiscountType string
	Amount       int64
	IsSubsidy    bool
}

func thb(satang int64) float64 { return float64(satang) / 100 }

// assemble stitches the batched rows into report orders, preserving the order
// of `orders` (already sorted newest-first by the query). Gross includes option
// price deltas, matching dashboard revenue.
func assemble(orders []OrderRow, items []ItemRow, options []OptionRow, discounts []DiscountRow) []domain.Order {
	optByItem := map[int32][]OptionRow{}
	for _, o := range options {
		optByItem[o.OrderItemID] = append(optByItem[o.OrderItemID], o)
	}
	itemsByOrder := map[int32][]ItemRow{}
	for _, it := range items {
		itemsByOrder[it.OrderID] = append(itemsByOrder[it.OrderID], it)
	}
	discByOrder := map[int32][]DiscountRow{}
	for _, d := range discounts {
		discByOrder[d.OrderID] = append(discByOrder[d.OrderID], d)
	}

	out := make([]domain.Order, 0, len(orders))
	for _, o := range orders {
		ord := domain.Order{
			Code:         o.Code,
			Status:       o.Status,
			CreatedAt:    o.CreatedAt,
			PaidAt:       o.PaidAt,
			MemberName:   o.MemberName,
			MemberPhone:  o.MemberPhone,
			PointsEarned: o.PointsEarned,
			LineItems:    []domain.LineItem{},
			Discounts:    []domain.DiscountLine{},
		}

		var grossSat, qty int64
		for _, it := range itemsByOrder[o.ID] {
			var optSat int64
			optLines := []domain.OptionLine{}
			for _, op := range optByItem[it.ID] {
				optSat += op.PriceDelta
				optLines = append(optLines, domain.OptionLine{Name: op.Name, PriceDelta: thb(op.PriceDelta)})
			}
			grossSat += (it.Price + optSat) * int64(it.Qty)
			qty += int64(it.Qty)
			ord.LineItems = append(ord.LineItems, domain.LineItem{
				Name:           it.Name,
				BaseOptionName: it.BaseOptionName,
				Qty:            it.Qty,
				Price:          thb(it.Price),
				Options:        optLines,
			})
		}
		ord.ItemCount = len(ord.LineItems)
		ord.Qty = qty

		var discSat, subSat int64
		for _, d := range discByOrder[o.ID] {
			if d.IsSubsidy {
				subSat += d.Amount
			} else {
				discSat += d.Amount
			}
			ord.Discounts = append(ord.Discounts, domain.DiscountLine{
				Name:         d.Name,
				DiscountType: d.DiscountType,
				Amount:       thb(d.Amount),
				IsSubsidy:    d.IsSubsidy,
			})
		}

		ord.Gross = thb(grossSat)
		ord.Discount = thb(discSat)
		ord.Subsidy = thb(subSat)
		ord.Net = thb(grossSat - discSat)
		out = append(out, ord)
	}
	return out
}
