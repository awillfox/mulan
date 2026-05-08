package http

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"mulan/sqlc"
)

func TestToMenuResponse(t *testing.T) {
	tests := []struct {
		name string
		in   sqlc.Menu
		want menuResponse
	}{
		{
			name: "all fields set",
			in: sqlc.Menu{
				ID:         1,
				Name:       "pad thai",
				Price:      8000,
				CategoryID: pgtype.Int4{Int32: 7, Valid: true},
				VfdName:    pgtype.Text{String: "PAD THAI", Valid: true},
				Active:     true,
			},
			want: menuResponse{
				ID:         1,
				Name:       "pad thai",
				Price:      80,
				CategoryID: ptr(int32(7)),
				VfdName:    ptr("PAD THAI"),
				Active:     true,
			},
		},
		{
			name: "null category and vfd",
			in: sqlc.Menu{
				ID:    2,
				Name:  "tea",
				Price: 2500,
			},
			want: menuResponse{
				ID:    2,
				Name:  "tea",
				Price: 25,
			},
		},
		{
			name: "zero price",
			in:   sqlc.Menu{ID: 3, Name: "free", Price: 0},
			want: menuResponse{ID: 3, Name: "free", Price: 0},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := toMenuResponse(tc.in)
			if got.ID != tc.want.ID || got.Name != tc.want.Name || got.Active != tc.want.Active {
				t.Fatalf("scalar mismatch: got %+v want %+v", got, tc.want)
			}
			if got.Price != tc.want.Price {
				t.Fatalf("price: got %v want %v", got.Price, tc.want.Price)
			}
			if !ptrEqual(got.CategoryID, tc.want.CategoryID) {
				t.Fatalf("category: got %v want %v", got.CategoryID, tc.want.CategoryID)
			}
			if !ptrEqual(got.VfdName, tc.want.VfdName) {
				t.Fatalf("vfd: got %v want %v", got.VfdName, tc.want.VfdName)
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
