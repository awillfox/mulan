package service

import (
	"strings"
	"testing"

	"mulan/sqlc"
)

func TestGenerateCode(t *testing.T) {
	const (
		want      = 8
		runs      = 100
		uniqueMin = 95
	)
	seen := make(map[string]struct{}, runs)
	for i := 0; i < runs; i++ {
		c, err := generateCode()
		if err != nil {
			t.Fatalf("generateCode: %v", err)
		}
		if len(c) != want {
			t.Fatalf("len = %d want %d", len(c), want)
		}
		for _, ch := range c {
			if !strings.ContainsRune(codeChars, ch) {
				t.Fatalf("char %q not in alphabet", ch)
			}
		}
		seen[c] = struct{}{}
	}
	if len(seen) < uniqueMin {
		t.Fatalf("low entropy: %d unique of %d", len(seen), runs)
	}
}

func TestCodeCharsAvoidsAmbiguous(t *testing.T) {
	bad := "01OI"
	for _, ch := range bad {
		if strings.ContainsRune(codeChars, ch) {
			t.Errorf("ambiguous char %q in alphabet", ch)
		}
	}
}

func TestResolveLineBase(t *testing.T) {
	baseOpts := []sqlc.MenuBaseOption{
		{ID: 10, MenuID: 1, Name: "Hot", Price: 5000},
		{ID: 11, MenuID: 1, Name: "Iced", Price: 8000},
	}
	tests := []struct {
		name      string
		menuPrice int64
		opts      []sqlc.MenuBaseOption
		baseID    int32
		wantPrice int64
		wantName  string
		wantErr   error
	}{
		{"no base opts, none chosen", 4500, nil, 0, 4500, "", nil},
		{"no base opts, id supplied -> reject", 4500, nil, 99, 0, "", ErrInvalidBaseOption},
		{"has base opts, valid pick", 0, baseOpts, 11, 8000, "Iced", nil},
		{"has base opts, none chosen -> reject", 0, baseOpts, 0, 0, "", ErrMissingBaseOption},
		{"has base opts, unknown id -> reject", 0, baseOpts, 999, 0, "", ErrInvalidBaseOption},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			price, name, err := resolveLineBase(tc.menuPrice, tc.opts, tc.baseID)
			if err != tc.wantErr {
				t.Fatalf("err = %v want %v", err, tc.wantErr)
			}
			if err == nil && (price != tc.wantPrice || name != tc.wantName) {
				t.Errorf("got (%d,%q) want (%d,%q)", price, name, tc.wantPrice, tc.wantName)
			}
		})
	}
}

func TestComputeDiscountAmountRoundsUpToBaht(t *testing.T) {
	tests := []struct {
		name  string
		dtype string
		value int64
		base  int64
		want  int64
	}{
		{"percent leftover satang rounds up", "percent", 700, 5500, 400}, // 7% of 55฿ = 3.85฿ -> 4฿
		{"percent already whole baht", "percent", 1000, 5000, 500},       // 10% of 50฿ = 5฿
		{"fixed with satang rounds up", "fixed", 1550, 9000, 1600},       // 15.50฿ -> 16฿
		{"fixed already whole baht", "fixed", 2000, 9000, 2000},          // 20฿
		{"discount over base clamps to base", "fixed", 5000, 3000, 3000}, // 50฿ off a 30฿ base -> 30฿
		{"zero base", "percent", 5000, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := computeDiscountAmount(sqlc.Discount{DiscountType: tc.dtype, Value: tc.value}, tc.base)
			if got != tc.want {
				t.Errorf("computeDiscountAmount = %d, want %d", got, tc.want)
			}
			if got%100 != 0 {
				t.Errorf("result %d is not a whole baht", got)
			}
		})
	}
}
