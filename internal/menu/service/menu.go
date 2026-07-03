package service

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	db "mulan/sqlc"
)

type MenuService struct {
	q *db.Queries
}

func NewMenuService(q *db.Queries) *MenuService {
	return &MenuService{q: q}
}

func (s *MenuService) List(ctx context.Context) ([]db.Menu, error) {
	return s.q.ListMenus(ctx)
}

func (s *MenuService) Create(ctx context.Context, name string, priceSatang int64, categoryID *int32, vfdName *string, favourite bool) (db.Menu, error) {
	catID := pgtype.Int4{}
	if categoryID != nil {
		catID = pgtype.Int4{Int32: *categoryID, Valid: true}
	}
	vfd := pgtype.Text{}
	if vfdName != nil && *vfdName != "" {
		vfd = pgtype.Text{String: *vfdName, Valid: true}
	}
	return s.q.CreateMenu(ctx, db.CreateMenuParams{
		Name:       name,
		Price:      priceSatang,
		CategoryID: catID,
		VfdName:    vfd,
		Favourite:  favourite,
	})
}

func (s *MenuService) Update(ctx context.Context, id int32, name string, priceSatang int64, categoryID *int32, vfdName *string, favourite bool) (db.Menu, error) {
	catID := pgtype.Int4{}
	if categoryID != nil {
		catID = pgtype.Int4{Int32: *categoryID, Valid: true}
	}
	vfd := pgtype.Text{}
	if vfdName != nil && *vfdName != "" {
		vfd = pgtype.Text{String: *vfdName, Valid: true}
	}
	return s.q.UpdateMenu(ctx, db.UpdateMenuParams{
		ID:         id,
		Name:       name,
		Price:      priceSatang,
		CategoryID: catID,
		VfdName:    vfd,
		Favourite:  favourite,
	})
}

func (s *MenuService) Toggle(ctx context.Context, id int32) (db.Menu, error) {
	return s.q.ToggleMenu(ctx, id)
}

func (s *MenuService) Delete(ctx context.Context, id int32) error {
	return s.q.DeleteMenu(ctx, id)
}

// Reorder assigns each menu in orderedIDs a 1-based sort_order within its
// category. Ids that don't belong to categoryID are ignored by the query.
func (s *MenuService) Reorder(ctx context.Context, categoryID *int32, orderedIDs []int32) error {
	catID := pgtype.Int4{}
	if categoryID != nil {
		catID = pgtype.Int4{Int32: *categoryID, Valid: true}
	}
	return s.q.SetMenuOrder(ctx, db.SetMenuOrderParams{
		Ids:        orderedIDs,
		CategoryID: catID,
	})
}
