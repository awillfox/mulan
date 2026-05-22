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

type CheckoutResult struct {
	Code       string
	Subtotal   float64 // THB
	VAT        float64 // THB
	VATPercent float64
	ShopName   string
	Total      float64 // THB
	Items      []CheckoutResultItem

	HasMember     bool
	MemberName    string
	MemberPhone   string
	PointsEarned  int64 // points awarded by this order
	PointsBalance int64 // member's running total after this order
}
