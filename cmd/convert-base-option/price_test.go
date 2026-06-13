package main

import "testing"

func TestBasePriceFor(t *testing.T) {
	tests := []struct {
		menuPrice, delta, want int64
		absolute               bool
	}{
		{5000, 0, 5000, false},    // Hot: 50฿ + 0
		{5000, 3000, 8000, false}, // Iced: 50฿ + 30
		{5000, -500, 4500, false}, // discounted serve
		{9500, 5500, 5500, true},  // absolute workaround: delta IS price
		{9500, 6500, 6500, true},  // absolute workaround: Iced
	}
	for _, tc := range tests {
		if got := basePriceFor(tc.menuPrice, tc.delta, tc.absolute); got != tc.want {
			t.Errorf("basePriceFor(%d,%d,abs=%v) = %d want %d", tc.menuPrice, tc.delta, tc.absolute, got, tc.want)
		}
	}
}

func TestIsAbsoluteGroup(t *testing.T) {
	relative := []optionPrice{{"Hot", 0}, {"Iced", 1500}, {"Frappe", 3000}}
	absolute := []optionPrice{{"Hot", 5500}, {"Iced", 6500}, {"Frappe", 8500}}
	noHot := []optionPrice{{"Iced", 0}, {"Frappe", 1000}}

	if isAbsoluteGroup(relative) {
		t.Error("relative group detected as absolute")
	}
	if !isAbsoluteGroup(absolute) {
		t.Error("absolute group not detected")
	}
	if isAbsoluteGroup(noHot) {
		t.Error("no-Hot group should not be absolute")
	}
}
