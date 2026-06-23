package main

import "strings"

const absoluteThreshold = 4000 // satang (40฿): Hot delta above this = absolute-price workaround

// isAbsoluteGroup returns true when the group's options were entered as
// absolute prices rather than deltas — detectable by a "Hot" option whose
// delta exceeds the threshold (a real delta would never be that large).
func isAbsoluteGroup(opts []optionPrice) bool {
	for _, o := range opts {
		if strings.EqualFold(o.Name, "hot") {
			return o.Delta > absoluteThreshold
		}
	}
	return false
}

type optionPrice struct {
	Name  string
	Delta int64
}

// basePriceFor returns the absolute satang price for a base option.
// When absolute is true the delta IS the price (workaround encoding);
// otherwise price = menuPrice + delta (normal relative encoding).
func basePriceFor(menuPrice, delta int64, absolute bool) int64 {
	if absolute {
		return delta
	}
	return menuPrice + delta
}
