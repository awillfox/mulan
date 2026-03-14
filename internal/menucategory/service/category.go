package service

import (
	"context"

	db "mulan/sqlc"
)

type CategoryService struct {
	q *db.Queries
}

func NewCategoryService(q *db.Queries) *CategoryService {
	return &CategoryService{q: q}
}

func (s *CategoryService) List(ctx context.Context) ([]db.MenuCategory, error) {
	return s.q.ListMenuCategories(ctx)
}

func (s *CategoryService) Create(ctx context.Context, name string) (db.MenuCategory, error) {
	return s.q.CreateMenuCategory(ctx, name)
}

func (s *CategoryService) Update(ctx context.Context, id int32, name string) (db.MenuCategory, error) {
	return s.q.UpdateMenuCategory(ctx, db.UpdateMenuCategoryParams{ID: id, Name: name})
}

func (s *CategoryService) Delete(ctx context.Context, id int32) error {
	return s.q.DeleteMenuCategory(ctx, id)
}
