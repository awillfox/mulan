package main

import "testing"

func TestBasePriceFor(t *testing.T) {
	tests := []struct {
		menuPrice, delta, want int64
	}{
		{5000, 0, 5000},    // Hot: 50฿ + 0
		{5000, 3000, 8000}, // Iced: 50฿ + 30
		{5000, -500, 4500}, // discounted serve
	}
	for _, tc := range tests {
		if got := basePriceFor(tc.menuPrice, tc.delta); got != tc.want {
			t.Errorf("basePriceFor(%d,%d) = %d want %d", tc.menuPrice, tc.delta, got, tc.want)
		}
	}
}
