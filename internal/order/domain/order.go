package domain

type OrderItem struct {
	MenuID int32
	Name   string
	Price  int64 // satang
	Qty    int32
}

type CheckoutResult struct {
	Code       string
	Subtotal   float64 // THB
	VAT        float64 // THB
	VATPercent float64
	ShopName   string
	Total      float64 // THB
	Items      []OrderItem
}
