package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDenomDeltaJSON(t *testing.T) {
	delta := map[int64]int{10000: 1, 500: -2}
	b, err := denomDeltaJSON(delta)
	if err != nil {
		t.Fatalf("denomDeltaJSON: %v", err)
	}
	var got map[string]int
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := map[string]int{"10000": 1, "500": -2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("json = %v, want %v", got, want)
	}
}

func TestTotalSatang(t *testing.T) {
	counts := map[int64]int{100000: 2, 10000: 3, 100: 5} // 2000 + 300 + 5 = 2305 baht
	if got := totalSatang(counts); got != 230500 {
		t.Fatalf("totalSatang = %d, want 230500", got)
	}
}

func TestChangeStockBaht(t *testing.T) {
	counts := map[int64]int{50000: 1, 2000: 3} // ฿500 x1, ฿20 x3
	got := changeStockBaht(counts)
	want := map[int]int{500: 1, 20: 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stock = %v, want %v", got, want)
	}
}

func TestIsCountUnderflow(t *testing.T) {
	if !isCountUnderflow(&pgconn.PgError{Code: "23514"}) {
		t.Error("check_violation (23514) should be treated as stock underflow")
	}
	if isCountUnderflow(&pgconn.PgError{Code: "23505"}) {
		t.Error("unique_violation (23505) is not a stock underflow")
	}
	if isCountUnderflow(errors.New("some other error")) {
		t.Error("plain error is not a stock underflow")
	}
	if isCountUnderflow(nil) {
		t.Error("nil is not a stock underflow")
	}
}
