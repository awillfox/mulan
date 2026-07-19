package service

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestAssemble(t *testing.T) {
	created := time.Date(2026, 6, 14, 15, 0, 0, 0, time.UTC)
	paid := pgtype.Timestamptz{Time: created.Add(3 * time.Minute), Valid: true}
	orders := []OrderRow{
		{ID: 1, Code: "AAA", Status: "paid", CreatedAt: created, PaidAt: paid, MemberName: "Cream", MemberPhone: "08", PointsEarned: 9},
		{ID: 2, Code: "BBB", Status: "open", CreatedAt: created, MemberName: "", MemberPhone: "", PointsEarned: 0},
	}
	items := []ItemRow{
		{ID: 10, OrderID: 1, Name: "Americano", BaseOptionName: "Iced", Price: 8000, Qty: 1},
		{ID: 20, OrderID: 2, Name: "Latte", BaseOptionName: "", Price: 5000, Qty: 2},
	}
	options := []OptionRow{
		{OrderItemID: 10, Name: "Oat milk", PriceDelta: 1000},
	}
	discounts := []DiscountRow{
		{OrderID: 1, Name: "Staff", DiscountType: "fixed", Amount: 2000, IsSubsidy: false},
	}

	got := assemble(orders, items, options, discounts)

	if len(got) != 2 {
		t.Fatalf("want 2 orders, got %d", len(got))
	}
	a := got[0]
	if a.Code != "AAA" {
		t.Fatalf("want AAA first, got %s", a.Code)
	}
	if !a.PaidAt.Valid || !a.PaidAt.Time.Equal(paid.Time) {
		t.Errorf("paid_at not carried through: %+v", a.PaidAt)
	}
	if a.Gross != 90.00 {
		t.Errorf("gross: want 90, got %v", a.Gross)
	}
	if a.Discount != 20.00 {
		t.Errorf("discount: want 20, got %v", a.Discount)
	}
	if a.Net != 70.00 {
		t.Errorf("net: want 70, got %v", a.Net)
	}
	if a.ItemCount != 1 || a.Qty != 1 {
		t.Errorf("counts: want 1/1, got %d/%d", a.ItemCount, a.Qty)
	}
	if len(a.LineItems) != 1 || len(a.LineItems[0].Options) != 1 || a.LineItems[0].Options[0].PriceDelta != 10.00 {
		t.Errorf("line/options not assembled: %+v", a.LineItems)
	}
	b := got[1]
	if b.PaidAt.Valid {
		t.Errorf("open order should have null paid_at, got %+v", b.PaidAt)
	}
	if b.Gross != 100.00 || b.Discount != 0 || b.Net != 100.00 {
		t.Errorf("open order: want gross 100/disc 0/net 100, got %v/%v/%v", b.Gross, b.Discount, b.Net)
	}
	if b.MemberName != "" {
		t.Errorf("walk-in member should be empty, got %q", b.MemberName)
	}
}

func TestSubsidySplit(t *testing.T) {
	got := assemble(
		[]OrderRow{{ID: 1, Code: "X", Status: "paid"}},
		[]ItemRow{{ID: 1, OrderID: 1, Name: "i", Price: 10000, Qty: 1}},
		nil,
		[]DiscountRow{
			{OrderID: 1, Name: "d", DiscountType: "fixed", Amount: 1000, IsSubsidy: false},
			{OrderID: 1, Name: "s", DiscountType: "fixed", Amount: 3000, IsSubsidy: true},
		},
	)
	if got[0].Discount != 10.00 || got[0].Subsidy != 30.00 || got[0].Net != 90.00 {
		t.Errorf("want disc 10/subsidy 30/net 90, got %v/%v/%v", got[0].Discount, got[0].Subsidy, got[0].Net)
	}
}
