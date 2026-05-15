package service

import (
	"context"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"mulan/sqlc"
)

// Logo holds the cached shop logo bytes for fast serving without a DB round
// trip on every /elements/logo.png hit. UpdatedAt drives ETag for browsers.
type Logo struct {
	Bytes     []byte
	MIME      string
	UpdatedAt time.Time
}

type SettingsService struct {
	q     *sqlc.Queries
	mu    sync.RWMutex
	cache sqlc.GetSettingsRow
	logo  Logo
}

func NewSettingsService(ctx context.Context, q *sqlc.Queries) (*SettingsService, error) {
	if err := q.SeedSettings(ctx); err != nil {
		return nil, err
	}
	row, err := q.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	svc := &SettingsService{q: q, cache: row}
	// Warm logo cache so the receipt-printer fetch hits memory on first call.
	if lrow, lerr := q.GetSettingsLogo(ctx); lerr == nil && len(lrow.Logo) > 0 {
		svc.logo = Logo{
			Bytes:     lrow.Logo,
			MIME:      lrow.LogoMime.String,
			UpdatedAt: lrow.UpdatedAt.Time,
		}
	}
	return svc, nil
}

// GetLogo returns the cached logo. Bytes is nil/empty when no logo is set.
func (s *SettingsService) GetLogo() Logo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.logo
}

// SetLogo writes new logo bytes + MIME and refreshes the cache. Caller must
// validate size and content-type before calling.
func (s *SettingsService) SetLogo(ctx context.Context, b []byte, mime string) error {
	var mimeArg pgtype.Text
	if mime != "" {
		mimeArg = pgtype.Text{String: mime, Valid: true}
	}
	if err := s.q.SetSettingsLogo(ctx, sqlc.SetSettingsLogoParams{
		Logo:     b,
		LogoMime: mimeArg,
	}); err != nil {
		return err
	}
	s.mu.Lock()
	s.logo = Logo{Bytes: b, MIME: mime, UpdatedAt: time.Now()}
	s.mu.Unlock()
	return nil
}

// ClearLogo removes the logo from DB and cache.
func (s *SettingsService) ClearLogo(ctx context.Context) error {
	if err := s.q.ClearSettingsLogo(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	s.logo = Logo{}
	s.mu.Unlock()
	return nil
}

func (s *SettingsService) Get() sqlc.GetSettingsRow {
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

func (s *SettingsService) Update(ctx context.Context, shopName string, vatPercent float64, receiptFooter string) (sqlc.GetSettingsRow, error) {
	row, err := s.q.UpdateSettings(ctx, sqlc.UpdateSettingsParams{
		ShopName:      shopName,
		VatPercent:    vatPercent,
		ReceiptFooter: receiptFooter,
	})
	if err != nil {
		return sqlc.GetSettingsRow{}, err
	}
	out := sqlc.GetSettingsRow{
		ID:            row.ID,
		ShopName:      row.ShopName,
		VatPercent:    row.VatPercent,
		ReceiptFooter: row.ReceiptFooter,
		UpdatedAt:     row.UpdatedAt,
	}
	s.mu.Lock()
	s.cache = out
	s.mu.Unlock()
	return out, nil
}
