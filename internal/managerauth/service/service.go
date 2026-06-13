package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"golang.org/x/crypto/bcrypt"

	"mulan/internal/managerauth/domain"
	"mulan/sqlc"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidSession     = errors.New("invalid or expired session")
	ErrUsernameTaken      = errors.New("username already in use")
	ErrInvalidRole        = errors.New("invalid role")
)

// sessionTTL is how long a freshly minted session stays valid.
const sessionTTL = 30 * 24 * time.Hour

type Service struct {
	q *sqlc.Queries
}

func NewService(q *sqlc.Queries) *Service {
	return &Service{q: q}
}

func pgtimestamptz(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// Login verifies username+password and, on success, mints a new session.
// Returns the RAW token (shown to the client once) and the session expiry.
func (s *Service) Login(ctx context.Context, username, password string) (domain.User, string, time.Time, error) {
	username = strings.TrimSpace(username)
	u, err := s.q.GetManagerUserByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, "", time.Time{}, ErrInvalidCredentials
		}
		return domain.User{}, "", time.Time{}, err
	}
	if !u.Active {
		return domain.User{}, "", time.Time{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return domain.User{}, "", time.Time{}, ErrInvalidCredentials
	}

	token, err := domain.GenerateToken()
	if err != nil {
		return domain.User{}, "", time.Time{}, err
	}
	expires := time.Now().Add(sessionTTL)
	if _, err := s.q.CreateManagerSession(ctx, sqlc.CreateManagerSessionParams{
		ManagerUserID: u.ID,
		TokenHash:     domain.HashToken(token),
		ExpiresAt:     pgtimestamptz(expires),
	}); err != nil {
		return domain.User{}, "", time.Time{}, err
	}
	return domain.User{ID: u.ID, Username: u.Username, Name: u.Name, Role: u.Role}, token, expires, nil
}

// Authenticate resolves a raw bearer token to the owning user, rejecting
// revoked, expired, or inactive-user sessions.
func (s *Service) Authenticate(ctx context.Context, token string) (domain.User, error) {
	if token == "" {
		return domain.User{}, ErrInvalidSession
	}
	row, err := s.q.GetManagerSessionWithUser(ctx, domain.HashToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, ErrInvalidSession
		}
		return domain.User{}, err
	}
	if row.RevokedAt.Valid {
		return domain.User{}, ErrInvalidSession
	}
	if !row.ExpiresAt.Valid || row.ExpiresAt.Time.Before(time.Now()) {
		return domain.User{}, ErrInvalidSession
	}
	if !row.Active {
		return domain.User{}, ErrInvalidSession
	}
	return domain.User{ID: row.UserID, Username: row.Username, Name: row.Name, Role: row.Role}, nil
}

// Logout revokes the session backing the given raw token. Idempotent.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.q.RevokeManagerSession(ctx, domain.HashToken(token))
}

// CreateUser provisions a manager account (used by the seed CLI). Validates the
// role and bcrypts the password.
func (s *Service) CreateUser(ctx context.Context, username, password, name, role string) (domain.User, error) {
	username = strings.TrimSpace(username)
	name = strings.TrimSpace(name)
	if !domain.ValidRole(role) {
		return domain.User{}, ErrInvalidRole
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 10)
	if err != nil {
		return domain.User{}, err
	}
	u, err := s.q.CreateManagerUser(ctx, sqlc.CreateManagerUserParams{
		Username:     username,
		PasswordHash: string(hash),
		Name:         name,
		Role:         role,
	})
	if err != nil {
		if strings.Contains(err.Error(), "manager_users_username_key") {
			return domain.User{}, ErrUsernameTaken
		}
		return domain.User{}, err
	}
	return domain.User{ID: u.ID, Username: u.Username, Name: u.Name, Role: u.Role}, nil
}
