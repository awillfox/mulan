package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"mulan/sqlc"
)

// Service is the CRUD layer for preset discounts. Discounts are managed in
// the manager UI and applied at POS checkout time (see the order service).
type Service struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

func NewService(pool *pgxpool.Pool, q *sqlc.Queries) *Service {
	return &Service{pool: pool, q: q}
}

// List returns every discount, active or not — used by the manager UI.
func (s *Service) List(ctx context.Context) ([]sqlc.Discount, error) {
	rows, err := s.q.ListDiscounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list discounts: %w", err)
	}
	return rows, nil
}

// ListActive returns only enabled discounts — used by the POS picker.
func (s *Service) ListActive(ctx context.Context) ([]sqlc.Discount, error) {
	rows, err := s.q.ListActiveDiscounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active discounts: %w", err)
	}
	return rows, nil
}

// Create inserts a new preset discount. value is satang for fixed discounts
// and hundredths-of-a-percent for percentage discounts (see schema.hcl).
func (s *Service) Create(ctx context.Context, name, dType string, value int64, active bool) (sqlc.Discount, error) {
	return s.q.CreateDiscount(ctx, sqlc.CreateDiscountParams{
		Name:         name,
		DiscountType: dType,
		Value:        value,
		Active:       active,
	})
}

func (s *Service) Update(ctx context.Context, id int32, name, dType string, value int64, active bool) (sqlc.Discount, error) {
	return s.q.UpdateDiscount(ctx, sqlc.UpdateDiscountParams{
		ID:           id,
		Name:         name,
		DiscountType: dType,
		Value:        value,
		Active:       active,
	})
}

func (s *Service) Delete(ctx context.Context, id int32) error {
	return s.q.DeleteDiscount(ctx, id)
}
