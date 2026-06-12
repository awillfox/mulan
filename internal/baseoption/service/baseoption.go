package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"mulan/sqlc"
)

type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewService(pool *pgxpool.Pool, q *sqlc.Queries) *Service {
	return &Service{pool: pool, q: q}
}

// BaseOption is one named, absolute-priced variant of a menu.
type BaseOption struct {
	ID    int32
	Name  string
	Price int64 // satang, absolute
}

// Spec is one base option in a replace-all request.
type Spec struct {
	Name  string
	Price int64 // satang
}

// ForMenus loads the base options attached to each of the given menus.
func (s *Service) ForMenus(ctx context.Context, menuIDs []int32) (map[int32][]BaseOption, error) {
	out := make(map[int32][]BaseOption, len(menuIDs))
	if len(menuIDs) == 0 {
		return out, nil
	}
	rows, err := s.q.ListBaseOptionsByMenuIDs(ctx, menuIDs)
	if err != nil {
		return nil, fmt.Errorf("list base options: %w", err)
	}
	for _, r := range rows {
		out[r.MenuID] = append(out[r.MenuID], BaseOption{ID: r.ID, Name: r.Name, Price: r.Price})
	}
	return out, nil
}

// SetForMenu replaces a menu's base options in one transaction. An empty
// specs slice removes all base options for the menu.
func (s *Service) SetForMenu(ctx context.Context, menuID int32, specs []Spec) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	if err := q.ClearMenuBaseOptions(ctx, menuID); err != nil {
		return fmt.Errorf("clear base options: %w", err)
	}
	for i, sp := range specs {
		if _, err := q.CreateMenuBaseOption(ctx, sqlc.CreateMenuBaseOptionParams{
			MenuID:    menuID,
			Name:      sp.Name,
			Price:     sp.Price,
			SortOrder: int32(i),
		}); err != nil {
			return fmt.Errorf("create base option: %w", err)
		}
	}
	return tx.Commit(ctx)
}
