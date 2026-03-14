package service

import (
	"context"

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
