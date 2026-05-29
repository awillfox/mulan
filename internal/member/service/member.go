package service

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"mulan/sqlc"
)

var (
	ErrNotFound       = errors.New("member not found")
	ErrDuplicatePhone = errors.New("phone already registered")
)

type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service {
	return &Service{q: q}
}

func textArg(s string) pgtype.Text {
	return pgtype.Text{String: s, Valid: s != ""}
}

func (s *Service) List(ctx context.Context, query string) ([]sqlc.Member, error) {
	return s.q.ListMembers(ctx, textArg(query))
}

func (s *Service) Lookup(ctx context.Context, phone string) (sqlc.Member, error) {
	m, err := s.q.FindMemberByPhone(ctx, phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Member{}, ErrNotFound
	}
	return m, err
}

func (s *Service) Create(ctx context.Context, phone, name string) (sqlc.Member, error) {
	m, err := s.q.CreateMember(ctx, sqlc.CreateMemberParams{
		Phone: phone,
		Name:  textArg(name),
	})
	if isUniqueViolation(err) {
		return sqlc.Member{}, ErrDuplicatePhone
	}
	return m, err
}

func (s *Service) Update(ctx context.Context, id int32, phone, name string) (sqlc.Member, error) {
	m, err := s.q.UpdateMember(ctx, sqlc.UpdateMemberParams{
		ID:    id,
		Phone: phone,
		Name:  textArg(name),
	})
	if isUniqueViolation(err) {
		return sqlc.Member{}, ErrDuplicatePhone
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlc.Member{}, ErrNotFound
	}
	return m, err
}

func (s *Service) Delete(ctx context.Context, id int32) error {
	return s.q.DeleteMember(ctx, id)
}

func (s *Service) Orders(ctx context.Context, id int32) ([]sqlc.ListOrdersByMemberRow, error) {
	return s.q.ListOrdersByMember(ctx, pgtype.Int4{Int32: id, Valid: true})
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
