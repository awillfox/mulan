package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"mulan/sqlc"
)

var (
	// ErrChangeNotMakeable is returned when the drawer cannot form the requested
	// change from current stock.
	ErrChangeNotMakeable = errors.New("cannot make exact change from drawer")
	// ErrUnknownDenomination is returned when a denomination key is not one of the
	// nine tracked denominations.
	ErrUnknownDenomination = errors.New("unknown denomination")
	// ErrNegativeCount is returned when an absolute count would be negative.
	ErrNegativeCount = errors.New("count must be >= 0")
	// ErrInsufficientStock is returned when an adjustment would drive a count below
	// zero (the DB CHECK fires).
	ErrInsufficientStock = errors.New("insufficient denomination stock")
)

// SeedDenominations inserts the nine tracked denomination rows if missing. Safe
// to call on every startup (ON CONFLICT DO NOTHING).
func (s *Service) SeedDenominations(ctx context.Context) error {
	for _, d := range DenominationsSatang {
		if err := s.q.SeedCashDrawerDenomination(ctx, int32(d)); err != nil {
			return fmt.Errorf("seed denom %d: %w", d, err)
		}
	}
	return nil
}

// CurrentDenoms returns the current count per denomination and the derived total
// in satang.
func (s *Service) CurrentDenoms(ctx context.Context) (map[int64]int, int64, error) {
	rows, err := s.q.ListCashDrawerDenominations(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list denominations: %w", err)
	}
	counts := make(map[int64]int, len(rows))
	for _, r := range rows {
		counts[int64(r.Denomination)] = int(r.Count)
	}
	return counts, totalSatang(counts), nil
}

// SetDenoms writes an absolute count for every supplied denomination, records the
// signed delta vs the previous state into the audit log, and returns the new
// counts + total. Unknown keys → ErrUnknownDenomination; negative → ErrNegativeCount.
// The pre-tx read assumes a single-terminal POS (no concurrent drawer writers).
func (s *Service) SetDenoms(ctx context.Context, counts map[int64]int, actor string) (map[int64]int, int64, error) {
	if err := validateDenomKeys(counts); err != nil {
		return nil, 0, err
	}
	for d, v := range counts {
		if v < 0 {
			return nil, 0, fmt.Errorf("%w: denom %d", ErrNegativeCount, d)
		}
	}
	prev, _, err := s.CurrentDenoms(ctx)
	if err != nil {
		return nil, 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	delta := make(map[int64]int)
	newCounts := make(map[int64]int, len(DenominationsSatang))
	var newTotal int64
	for _, d := range DenominationsSatang {
		newC, ok := counts[d]
		if !ok {
			newC = prev[d]
		}
		if diff := newC - prev[d]; diff != 0 {
			delta[d] = diff
		}
		if err := q.SetCashDrawerDenomination(ctx, sqlc.SetCashDrawerDenominationParams{
			Denomination: int32(d),
			Count:        int32(newC),
		}); err != nil {
			return nil, 0, fmt.Errorf("set denom %d: %w", d, err)
		}
		newCounts[d] = newC
		newTotal += d * int64(newC)
	}
	if err := appendDenomAudit(ctx, q, "set", delta, actor); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit: %w", err)
	}
	return newCounts, newTotal, nil
}

// AdjustDenoms applies a relative change per denomination and returns the new
// counts + total. A subtraction that would drive a count below zero fails the tx
// (DB CHECK) and is surfaced as ErrInsufficientStock. The pre-tx state assumes a
// single-terminal POS (no concurrent drawer writers).
func (s *Service) AdjustDenoms(ctx context.Context, delta map[int64]int, actor string) (map[int64]int, int64, error) {
	if err := validateDenomKeys(delta); err != nil {
		return nil, 0, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	applied := make(map[int64]int)
	for _, d := range DenominationsSatang {
		diff, ok := delta[d]
		if !ok || diff == 0 {
			continue
		}
		if _, err := q.AdjustCashDrawerDenomination(ctx, sqlc.AdjustCashDrawerDenominationParams{
			Denomination: int32(d),
			Delta:        int32(diff),
		}); err != nil {
			if strings.Contains(err.Error(), "cash_drawer_denominations_count_nonneg") {
				return nil, 0, fmt.Errorf("%w: denom %d", ErrInsufficientStock, d)
			}
			return nil, 0, fmt.Errorf("adjust denom %d: %w", d, err)
		}
		applied[d] = diff
	}
	if err := appendDenomAudit(ctx, q, "adjust", applied, actor); err != nil {
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit: %w", err)
	}
	newCounts, total, err := s.CurrentDenoms(ctx)
	if err != nil {
		return nil, 0, err
	}
	return newCounts, total, nil
}

// MakeChange computes the bills/coins to return for changeSatang against current
// stock. changeSatang must be a whole-baht multiple (cash due is rounded to ฿1).
// Returns ErrChangeNotMakeable when the drawer cannot form it.
func (s *Service) MakeChange(ctx context.Context, changeSatang int64) (map[int64]int, error) {
	counts, _, err := s.CurrentDenoms(ctx)
	if err != nil {
		return nil, err
	}
	breakBaht, ok := makeChangeBaht(int(changeSatang/100), changeStockBaht(counts))
	if !ok {
		return nil, ErrChangeNotMakeable
	}
	out := make(map[int64]int, len(breakBaht))
	for dBaht, n := range breakBaht {
		out[int64(dBaht)*100] = n
	}
	return out, nil
}

// ApplyCashSale adds the tendered denominations and removes the change
// denominations from the drawer, then appends a 'sale' audit row with the net
// delta. It MUST be called with a tx-bound *sqlc.Queries (the checkout tx) so the
// drawer movement commits atomically with the order. A change denom exceeding
// stock fails the tx via the CHECK; callers run MakeChange first so this is an
// invariant guard, not the primary feasibility check.
func (s *Service) ApplyCashSale(ctx context.Context, q *sqlc.Queries, tender, change map[int64]int, actor string) error {
	net := make(map[int64]int)
	for d, n := range tender {
		net[d] += n
	}
	for d, n := range change {
		net[d] -= n
	}
	for _, d := range DenominationsSatang {
		diff := net[d]
		if diff == 0 {
			continue
		}
		if _, err := q.AdjustCashDrawerDenomination(ctx, sqlc.AdjustCashDrawerDenominationParams{
			Denomination: int32(d),
			Delta:        int32(diff),
		}); err != nil {
			return fmt.Errorf("apply cash sale denom %d: %w", d, err)
		}
	}
	return appendDenomAudit(ctx, q, "sale", net, actor)
}

// ── helpers ────────────────────────────────────────────────────────

func totalSatang(counts map[int64]int) int64 {
	var t int64
	for d, c := range counts {
		t += d * int64(c)
	}
	return t
}

func changeStockBaht(counts map[int64]int) map[int]int {
	out := make(map[int]int, len(counts))
	for d, c := range counts {
		out[int(d/100)] = c
	}
	return out
}

// validDenoms is the set of accepted denomination keys, built once at init so
// validateDenomKeys pays no allocation cost per call.
var validDenoms = func() map[int64]struct{} {
	m := make(map[int64]struct{}, len(DenominationsSatang))
	for _, d := range DenominationsSatang {
		m[d] = struct{}{}
	}
	return m
}()

func validateDenomKeys(m map[int64]int) error {
	for d := range m {
		if _, ok := validDenoms[d]; !ok {
			return fmt.Errorf("%w: %d", ErrUnknownDenomination, d)
		}
	}
	return nil
}

func denomDeltaJSON(delta map[int64]int) ([]byte, error) {
	m := make(map[string]int, len(delta))
	for d, n := range delta {
		m[strconv.FormatInt(d, 10)] = n
	}
	return json.Marshal(m)
}

// appendDenomAudit writes an audit row carrying the signed per-denom delta and
// the net satang amount moved.
func appendDenomAudit(ctx context.Context, q *sqlc.Queries, eventType string, delta map[int64]int, actor string) error {
	js, err := denomDeltaJSON(delta)
	if err != nil {
		return fmt.Errorf("encode denom delta: %w", err)
	}
	var net int64
	for d, n := range delta {
		net += d * int64(n)
	}
	var actorText pgtype.Text
	if actor != "" {
		actorText = pgtype.Text{String: actor, Valid: true}
	}
	_, err = q.AppendCashDrawerDenomEvent(ctx, sqlc.AppendCashDrawerDenomEventParams{
		EventType:     eventType,
		Delta:         pgtype.Int8{Int64: net, Valid: true},
		Actor:         actorText,
		Denominations: js,
	})
	if err != nil {
		return fmt.Errorf("append %s audit: %w", eventType, err)
	}
	return nil
}
