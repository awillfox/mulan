// Package service owns the cash-drawer audit log and float state.
//
// Storage is event-sourced: every state-changing action (set float, clear,
// kick, open-for-change) is appended to cash_drawer_audit. The "current
// float" is derived by reading the most recent set/clear event. Kicks do not
// carry an amount — they only log that the physical drawer opened.
package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"mulan/sqlc"
)

// Event types persisted in cash_drawer_audit.event_type. Must match the CHECK
// constraint declared in schema.hcl.
const (
	EventSet           = "set"
	EventClear         = "clear"
	EventAdjust        = "adjust"
	EventKick          = "kick"
	EventOpenForChange = "open_for_change"
)

var ErrInvalidAmount = errors.New("amount must be >= 0")

// CurrentFloat captures the latest "set" or "clear" event for display.
// Amount is in satang. AmountValid is false when no float has ever been set.
type CurrentFloat struct {
	EventType   string
	Amount      int64
	AmountValid bool
	SetAt       pgtype.Timestamptz
}

// AuditEvent mirrors sqlc.CashDrawerAudit but with nullable cols unwrapped to
// pointer-of-primitive so the HTTP layer can emit clean JSON.
type AuditEvent struct {
	ID        int64
	EventType string
	Amount    *int64
	Delta     *int64
	Note      *string
	Actor     *string
	Terminal  *string
	CreatedAt pgtype.Timestamptz
}

type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service {
	return &Service{q: q}
}

// SetFloat records an absolute float reading. Pass amount in satang. Delta is
// computed against the current float (or zero when this is the first reading)
// so the audit log captures the cash movement.
func (s *Service) SetFloat(ctx context.Context, amountSatang int64, note, actor, terminal *string) (AuditEvent, error) {
	if amountSatang < 0 {
		return AuditEvent{}, ErrInvalidAmount
	}
	prev, err := s.Current(ctx)
	if err != nil {
		return AuditEvent{}, err
	}
	var delta pgtype.Int8
	if prev.AmountValid {
		delta = pgtype.Int8{Int64: amountSatang - prev.Amount, Valid: true}
	}
	row, err := s.q.AppendCashDrawerEvent(ctx, sqlc.AppendCashDrawerEventParams{
		EventType: EventSet,
		Amount:    pgtype.Int8{Int64: amountSatang, Valid: true},
		Delta:     delta,
		Note:      textOpt(note),
		Actor:     textOpt(actor),
		Terminal:  textOpt(terminal),
	})
	if err != nil {
		return AuditEvent{}, fmt.Errorf("append set: %w", err)
	}
	return toAuditEvent(row), nil
}

// ClearFloat logs a clear event (resets the recorded float to 0). Useful at
// shift end or when reconciling.
func (s *Service) ClearFloat(ctx context.Context, note, actor, terminal *string) (AuditEvent, error) {
	prev, err := s.Current(ctx)
	if err != nil {
		return AuditEvent{}, err
	}
	var delta pgtype.Int8
	if prev.AmountValid {
		delta = pgtype.Int8{Int64: -prev.Amount, Valid: true}
	}
	row, err := s.q.AppendCashDrawerEvent(ctx, sqlc.AppendCashDrawerEventParams{
		EventType: EventClear,
		Amount:    pgtype.Int8{Int64: 0, Valid: true},
		Delta:     delta,
		Note:      textOpt(note),
		Actor:     textOpt(actor),
		Terminal:  textOpt(terminal),
	})
	if err != nil {
		return AuditEvent{}, fmt.Errorf("append clear: %w", err)
	}
	return toAuditEvent(row), nil
}

// LogKick records a manual or automated drawer open. No amount is attached;
// reason flows into the note field.
func (s *Service) LogKick(ctx context.Context, eventType string, note, actor, terminal *string) (AuditEvent, error) {
	if eventType != EventKick && eventType != EventOpenForChange {
		eventType = EventKick
	}
	row, err := s.q.AppendCashDrawerEvent(ctx, sqlc.AppendCashDrawerEventParams{
		EventType: eventType,
		Note:      textOpt(note),
		Actor:     textOpt(actor),
		Terminal:  textOpt(terminal),
	})
	if err != nil {
		return AuditEvent{}, fmt.Errorf("append kick: %w", err)
	}
	return toAuditEvent(row), nil
}

// Current returns the latest set/clear event so the UI can show the running
// float. If no such event exists, AmountValid is false.
func (s *Service) Current(ctx context.Context) (CurrentFloat, error) {
	row, err := s.q.GetCurrentCashDrawerFloat(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return CurrentFloat{}, nil
		}
		return CurrentFloat{}, fmt.Errorf("read current float: %w", err)
	}
	out := CurrentFloat{
		EventType: row.EventType,
		SetAt:     row.CreatedAt,
	}
	if row.Amount.Valid {
		out.Amount = row.Amount.Int64
		out.AmountValid = true
	}
	return out, nil
}

// ListAudit returns paginated audit history, newest first.
func (s *Service) ListAudit(ctx context.Context, limit, offset int32) ([]AuditEvent, int64, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.q.ListCashDrawerAudit(ctx, sqlc.ListCashDrawerAuditParams{Limit: limit, Offset: offset})
	if err != nil {
		return nil, 0, fmt.Errorf("list audit: %w", err)
	}
	total, err := s.q.CountCashDrawerAudit(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count audit: %w", err)
	}
	out := make([]AuditEvent, len(rows))
	for i, r := range rows {
		out[i] = toAuditEvent(r)
	}
	return out, total, nil
}

func textOpt(p *string) pgtype.Text {
	if p == nil || *p == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: *p, Valid: true}
}

func toAuditEvent(r sqlc.CashDrawerAudit) AuditEvent {
	out := AuditEvent{
		ID:        r.ID,
		EventType: r.EventType,
		CreatedAt: r.CreatedAt,
	}
	if r.Amount.Valid {
		v := r.Amount.Int64
		out.Amount = &v
	}
	if r.Delta.Valid {
		v := r.Delta.Int64
		out.Delta = &v
	}
	if r.Note.Valid {
		v := r.Note.String
		out.Note = &v
	}
	if r.Actor.Valid {
		v := r.Actor.String
		out.Actor = &v
	}
	if r.Terminal.Valid {
		v := r.Terminal.String
		out.Terminal = &v
	}
	return out
}
