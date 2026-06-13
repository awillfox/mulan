package service

import "testing"

func TestComputeOrderTotals(t *testing.T) {
	tests := []struct {
		name                                        string
		gross, normalDisc, subsidy                  int64
		vatPercent                                  float64
		wantShopReceives, wantCustomerPays, wantVAT int64
	}{
		// ฿100 item, 50% subsidy, VAT 7%: customer pays ฿50, shop whole at ฿100,
		// VAT is the inclusive portion of ฿100 = 10000 - floor(10000*10000/10700) = 655 satang.
		{"subsidy only", 10000, 0, 5000, 7, 10000, 5000, 655},
		// Same item, normal 50% discount: shop earns ฿50, VAT on ฿50 = 5000 - floor(5000*10000/10700) = 328.
		{"normal only", 10000, 5000, 0, 7, 5000, 5000, 328},
		// Mixed: ฿20 normal + ฿30 subsidy. Shop receives ฿80, customer pays ฿50.
		{"mixed", 10000, 2000, 3000, 7, 8000, 5000, 524},
		// VAT off: no VAT, subsidy still reduces customer total.
		{"vat off", 10000, 0, 5000, 0, 10000, 5000, 0},
		// No discounts: shop receives == customer pays == gross.
		{"plain", 10000, 0, 0, 7, 10000, 10000, 655},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeOrderTotals(tc.gross, tc.normalDisc, tc.subsidy, tc.vatPercent)
			if got.ShopReceives != tc.wantShopReceives {
				t.Errorf("ShopReceives = %d, want %d", got.ShopReceives, tc.wantShopReceives)
			}
			if got.CustomerPays != tc.wantCustomerPays {
				t.Errorf("CustomerPays = %d, want %d", got.CustomerPays, tc.wantCustomerPays)
			}
			if got.VAT != tc.wantVAT {
				t.Errorf("VAT = %d, want %d", got.VAT, tc.wantVAT)
			}
			if got.NormalDisc != tc.normalDisc || got.Subsidy != tc.subsidy || got.Gross != tc.gross {
				t.Errorf("passthrough fields mismatch: %+v", got)
			}
		})
	}
}
