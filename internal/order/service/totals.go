package service

// orderTotalsResult is the satang breakdown computed at checkout. All amounts
// are VAT-inclusive THB-in-satang (menu prices already include VAT).
type orderTotalsResult struct {
	Gross        int64 // sum of price*qty (VAT-inclusive)
	NormalDisc   int64 // discounts that reduce the shop's revenue
	Subsidy      int64 // sponsor-covered discounts (shop is made whole)
	ShopReceives int64 // gross - normalDisc (the VAT base; full pre-subsidy)
	CustomerPays int64 // gross - normalDisc - subsidy
	VAT          int64 // inclusive VAT portion of ShopReceives
}

// computeOrderTotals splits discounts into normal vs subsidy and computes
// VAT-inclusive totals. Subsidies do NOT reduce the VAT base — VAT is reckoned
// on the full pre-subsidy amount the shop receives; subsidies only reduce what
// the customer pays. vatPercent is the whole-number percent (7 => 7%).
func computeOrderTotals(gross, normalDisc, subsidy int64, vatPercent float64) orderTotalsResult {
	shopReceives := gross - normalDisc
	customerPays := shopReceives - subsidy
	var vat int64
	if vatPercent > 0 && shopReceives > 0 {
		ratePer10000 := int64(vatPercent * 100) // 7% -> 700
		net := shopReceives * 10000 / (10000 + ratePer10000)
		vat = shopReceives - net
	}
	return orderTotalsResult{
		Gross:        gross,
		NormalDisc:   normalDisc,
		Subsidy:      subsidy,
		ShopReceives: shopReceives,
		CustomerPays: customerPays,
		VAT:          vat,
	}
}
