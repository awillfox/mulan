package domain

type SelectedOption struct {
	ID         int32
	Name       string
	PriceDelta int64 // satang
}

type OrderItem struct {
	MenuID  int32
	Name    string
	Price   int64 // satang
	Qty     int32
	Options []SelectedOption
}

type CheckoutResultItem struct {
	Name    string
	Price   int64 // satang, base price per unit
	Qty     int32
	Options []SelectedOption
}

// AppliedDiscount is one discount that was applied to a checked-out order,
// snapshotted with the satang amount it actually removed.
type AppliedDiscount struct {
	DiscountID int32
	Name       string
	Type       string // "fixed" | "percent"
	Amount     int64  // satang, positive
}

type CheckoutResult struct {
	Code          string
	Subtotal      float64 // THB, before discounts
	Discount      float64 // THB, total of all discounts applied
	VAT           float64 // THB
	VATPercent    float64
	ShopName      string
	ReceiptFooter string
	Total         float64 // THB, after discounts + VAT
	Items         []CheckoutResultItem
	Discounts     []AppliedDiscount
}
