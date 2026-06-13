package service

import (
	"reflect"
	"testing"
)

func TestMakeChangeBreakdown(t *testing.T) {
	tests := []struct {
		name       string
		changeBaht int
		stock      map[int]int // denomBaht -> count
		wantOK     bool
		wantBreak  map[int]int
	}{
		{
			name:       "zero change is trivially makeable, empty breakdown",
			changeBaht: 0,
			stock:      map[int]int{20: 5},
			wantOK:     true,
			wantBreak:  map[int]int{},
		},
		{
			name:       "exact with plenty of stock uses fewest coins",
			changeBaht: 67,
			stock:      map[int]int{50: 2, 10: 5, 5: 5, 2: 5, 1: 5},
			wantOK:     true,
			wantBreak:  map[int]int{50: 1, 10: 1, 5: 1, 2: 1}, // 50+10+5+2 = 67
		},
		{
			name:       "greedy would fail but DP finds it",
			changeBaht: 60,
			stock:      map[int]int{50: 1, 20: 3}, // greedy takes 50 then stuck; 20*3 works
			wantOK:     true,
			wantBreak:  map[int]int{20: 3},
		},
		{
			name:       "infeasible: cannot reach amount with stock",
			changeBaht: 8,
			stock:      map[int]int{50: 1}, // no small coins
			wantOK:     false,
			wantBreak:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotBreak, gotOK := makeChangeBaht(tc.changeBaht, tc.stock)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if tc.wantOK && !reflect.DeepEqual(gotBreak, tc.wantBreak) {
				t.Fatalf("breakdown = %v, want %v", gotBreak, tc.wantBreak)
			}
		})
	}
}
