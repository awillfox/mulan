package http

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	baseoptionservice "mulan/internal/baseoption/service"
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
				SortOrder:  4,
			},
			want: menuResponse{
				ID:         1,
				Name:       "pad thai",
				Price:      80,
				CategoryID: ptr(int32(7)),
				VfdName:    ptr("PAD THAI"),
				Active:     true,
				SortOrder:  4,
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
			if got.SortOrder != tc.want.SortOrder {
				t.Fatalf("sort_order: got %v want %v", got.SortOrder, tc.want.SortOrder)
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

func TestToMenuBaseOptions(t *testing.T) {
	in := []baseoptionservice.BaseOption{
		{ID: 1, Name: "Hot", Price: 5000},
		{ID: 2, Name: "Iced", Price: 8000},
	}
	got := toMenuBaseOptions(in)
	if len(got) != 2 {
		t.Fatalf("len = %d want 2", len(got))
	}
	if got[0].Name != "Hot" || got[0].Price != 50 {
		t.Errorf("got[0] = %+v want {1 Hot 50}", got[0])
	}
	if got[1].Name != "Iced" || got[1].Price != 80 {
		t.Errorf("got[1] = %+v want {2 Iced 80}", got[1])
	}
}

func ptr[T any](v T) *T { return &v }

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
