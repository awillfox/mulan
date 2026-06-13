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
	Name           string
	Price          int64 // satang, base price per unit
	Qty            int32
	Options        []SelectedOption
	BaseOptionName string // chosen base option name, empty when none
}

// AppliedDiscount is one discount that was applied to a checked-out order,
// snapshotted with the satang amount it actually removed.
type AppliedDiscount struct {
	DiscountID int32
	Name       string
	Type       string // "fixed" | "percent"
	Amount     int64  // satang, positive
	IsSubsidy  bool   // sponsor-covered (shop made whole) vs normal discount
}

type CheckoutResult struct {
	OrderID       int32
	Code          string
	Subtotal      float64 // THB, before discounts
	Discount      float64 // THB, normal (shop-absorbed) discounts only; sponsor subsidies are in Subsidy
	Subsidy       float64 // THB, total sponsor-covered (shop made whole); customer savings = Discount + Subsidy
	VAT           float64 // THB
	VATPercent    float64
	ShopName      string
	ReceiptFooter string
	Total         float64 // THB, after discounts + VAT
	Items         []CheckoutResultItem
	Discounts     []AppliedDiscount
	HasMember     bool
	MemberName    string
	MemberPhone   string
	PointsEarned  int64
	PointsBalance int64

	RoundedDue      float64        `json:"rounded_due"`
	Change          float64        `json:"change"`
	ChangeBreakdown map[string]int `json:"change_breakdown"`
}
