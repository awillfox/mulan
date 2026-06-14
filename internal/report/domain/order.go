package domain

import "time"

// OptionLine is a chosen option snapshotted on an order line. Price in THB.
type OptionLine struct {
	Name       string  `json:"name"`
	PriceDelta float64 `json:"price_delta"`
}

// LineItem is one order line. Price (THB) is the snapshotted base/line price;
// Options carry +deltas (THB) shown in detail.
type LineItem struct {
	Name           string       `json:"name"`
	BaseOptionName string       `json:"base_option_name"`
	Qty            int32        `json:"qty"`
	Price          float64      `json:"price"`
	Options        []OptionLine `json:"options"`
}

// DiscountLine is an applied discount snapshot. Amount in THB.
type DiscountLine struct {
	Name         string  `json:"name"`
	DiscountType string  `json:"discount_type"`
	Amount       float64 `json:"amount"`
	IsSubsidy    bool    `json:"is_subsidy"`
}

// Order is one row of the orders report. Money fields are THB.
type Order struct {
	Code         string         `json:"code"`
	Status       string         `json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	MemberName   string         `json:"member_name"`
	MemberPhone  string         `json:"member_phone"`
	PointsEarned int64          `json:"points_earned"`
	ItemCount    int            `json:"item_count"`
	Qty          int64          `json:"qty"`
	Gross        float64        `json:"gross"`
	Discount     float64        `json:"discount"`
	Subsidy      float64        `json:"subsidy"`
	Net          float64        `json:"net"`
	LineItems    []LineItem     `json:"line_items"`
	Discounts    []DiscountLine `json:"discounts"`
}

// Page is the paginated response body.
type Page struct {
	Orders []Order `json:"orders"`
	Total  int64   `json:"total"`
}
