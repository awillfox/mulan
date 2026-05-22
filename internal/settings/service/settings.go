package service

import (
	"context"
	"sync"

	"mulan/sqlc"
)

type SettingsService struct {
	q     *sqlc.Queries
	mu    sync.RWMutex
	cache sqlc.Setting
}

func NewSettingsService(ctx context.Context, q *sqlc.Queries) (*SettingsService, error) {
	if err := q.SeedSettings(ctx); err != nil {
		return nil, err
	}
	row, err := q.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &SettingsService{q: q, cache: row}, nil
}

func (s *SettingsService) Get() sqlc.Setting {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cache
}

func (s *SettingsService) ShopName() string {
	return s.Get().ShopName
}

func (s *SettingsService) VATPercent() float64 {
	return s.Get().VatPercent
}

func (s *SettingsService) PointsPerBaht() float64 {
	return s.Get().PointsPerBaht
}

func (s *SettingsService) Update(ctx context.Context, shopName string, vatPercent, pointsPerBaht float64) (sqlc.Setting, error) {
	row, err := s.q.UpdateSettings(ctx, sqlc.UpdateSettingsParams{
		ShopName:      shopName,
		VatPercent:    vatPercent,
		PointsPerBaht: pointsPerBaht,
	})
	if err != nil {
		return sqlc.Setting{}, err
	}
	s.mu.Lock()
	s.cache = row
	s.mu.Unlock()
	return row, nil
}
