package main

// basePriceFor converts a legacy delta-based option into the absolute base
// price: the menu's price is the price of the zero-delta serve, so the menu
// price plus the option's delta is exactly what the customer paid for that
// serve. Both values are satang.
func basePriceFor(menuPrice, delta int64) int64 {
	return menuPrice + delta
}
