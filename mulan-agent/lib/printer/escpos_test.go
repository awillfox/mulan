package printer

import "testing"

func TestOrderItemDisplayName(t *testing.T) {
	tests := []struct {
		name string
		item OrderItem
		want string
	}{
		{"with base option", OrderItem{Name: "Americano", BaseOptionName: "Iced"}, "Americano (Iced)"},
		{"no base option", OrderItem{Name: "Croissant"}, "Croissant"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.item.displayName(); got != tc.want {
				t.Errorf("displayName() = %q want %q", got, tc.want)
			}
		})
	}
}
