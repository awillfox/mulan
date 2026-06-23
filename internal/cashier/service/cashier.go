package service

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"mulan/sqlc"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrLoginIDTaken       = errors.New("login ID already in use")
	ErrCashierNotFound    = errors.New("cashier not found")
	ErrNotManager         = errors.New("cashier is not a manager")
	ErrInvalidRole        = errors.New("invalid role")
)

const (
	RoleCashier = "cashier"
	RoleManager = "manager"
)

func validRole(r string) bool {
	return r == RoleCashier || r == RoleManager
}

type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service {
	return &Service{q: q}
}

func (s *Service) Login(ctx context.Context, loginID, pin string) (sqlc.Cashier, error) {
	loginID = strings.TrimSpace(loginID)
	c, err := s.q.GetCashierByLoginID(ctx, loginID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Cashier{}, ErrInvalidCredentials
		}
		return sqlc.Cashier{}, err
	}
	if !c.Active {
		return sqlc.Cashier{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(c.PinHash), []byte(pin)); err != nil {
		return sqlc.Cashier{}, ErrInvalidCredentials
	}
	return c, nil
}

func (s *Service) List(ctx context.Context) ([]sqlc.Cashier, error) {
	return s.q.ListCashiers(ctx)
}

func (s *Service) Create(ctx context.Context, loginID, name, pin, role string) (sqlc.Cashier, error) {
	loginID = strings.TrimSpace(loginID)
	name = strings.TrimSpace(name)
	if role == "" {
		role = RoleCashier
	}
	if !validRole(role) {
		return sqlc.Cashier{}, ErrInvalidRole
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), 10)
	if err != nil {
		return sqlc.Cashier{}, err
	}
	c, err := s.q.CreateCashier(ctx, sqlc.CreateCashierParams{
		LoginID: loginID,
		Name:    name,
		PinHash: string(hash),
		Role:    role,
	})
	if err != nil && strings.Contains(err.Error(), "cashiers_login_id_key") {
		return sqlc.Cashier{}, ErrLoginIDTaken
	}
	return c, err
}

func (s *Service) Update(ctx context.Context, id int32, name string, active bool, role string) (sqlc.Cashier, error) {
	if role == "" {
		// Preserve the existing role: an update that omits role (e.g. an
		// active-only toggle) must not silently demote a manager to cashier.
		existing, err := s.q.GetCashier(ctx, id)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return sqlc.Cashier{}, ErrCashierNotFound
			}
			return sqlc.Cashier{}, err
		}
		role = existing.Role
	}
	if !validRole(role) {
		return sqlc.Cashier{}, ErrInvalidRole
	}
	c, err := s.q.UpdateCashier(ctx, sqlc.UpdateCashierParams{
		ID:     id,
		Name:   strings.TrimSpace(name),
		Active: active,
		Role:   role,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Cashier{}, ErrCashierNotFound
		}
		return sqlc.Cashier{}, err
	}
	return c, nil
}

func (s *Service) UpdatePin(ctx context.Context, id int32, pin string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), 10)
	if err != nil {
		return err
	}
	_, err = s.q.UpdateCashierPin(ctx, sqlc.UpdateCashierPinParams{
		ID:      id,
		PinHash: string(hash),
	})
	if err != nil && errors.Is(err, pgx.ErrNoRows) {
		return ErrCashierNotFound
	}
	return err
}

func (s *Service) Delete(ctx context.Context, id int32) error {
	return s.q.DeleteCashier(ctx, id)
}

// VerifyManager authorizes a drawer write from the POS by id + PIN. It is meant
// for a trusted local POS kiosk on the tailnet, NOT as a general-purpose
// credential gate. It loads a cashier by id and confirms it is an active manager
// whose PIN matches. Returns ErrInvalidCredentials on bad id/pin and
// ErrNotManager when the cashier exists but lacks the manager role.
func (s *Service) VerifyManager(ctx context.Context, id int32, pin string) (sqlc.Cashier, error) {
	c, err := s.q.GetCashier(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return sqlc.Cashier{}, ErrInvalidCredentials
		}
		return sqlc.Cashier{}, err
	}
	if !c.Active {
		return sqlc.Cashier{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(c.PinHash), []byte(pin)); err != nil {
		return sqlc.Cashier{}, ErrInvalidCredentials
	}
	if c.Role != RoleManager {
		return sqlc.Cashier{}, ErrNotManager
	}
	return c, nil
}
