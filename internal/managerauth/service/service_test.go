package service

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"mulan/internal/managerauth/domain"
	"mulan/sqlc"
)

func newTestService(t *testing.T) (*Service, *pgxpool.Pool) {
	t.Helper()
	// This repo has a single database (PSQL_URL); there is no separate dev DB,
	// so fall back to PSQL_URL when PSQL_DEV_URL is unset. Skip only when neither
	// is available, so `. ./.env && go test ./...` actually exercises the DB.
	url := os.Getenv("PSQL_DEV_URL")
	if url == "" {
		url = os.Getenv("PSQL_URL")
	}
	if url == "" {
		t.Skip("neither PSQL_DEV_URL nor PSQL_URL set; skipping DB integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	return NewService(sqlc.New(pool)), pool
}

func TestLoginAuthenticateLogout(t *testing.T) {
	svc, pool := newTestService(t)
	defer pool.Close()
	ctx := context.Background()

	username := "test_owner_" + time.Now().Format("150405.000000")
	if _, err := svc.CreateUser(ctx, username, "s3cret-pass", "Test Owner", domain.RoleOwner); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	// Single shared DB: clean up the test user (FK cascade removes its sessions).
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM manager_users WHERE username = $1", username)
	})

	if _, _, _, err := svc.Login(ctx, username, "wrong"); err != ErrInvalidCredentials {
		t.Fatalf("Login(wrong) err = %v, want ErrInvalidCredentials", err)
	}

	user, token, expires, err := svc.Login(ctx, username, "s3cret-pass")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if user.Role != domain.RoleOwner || token == "" || !expires.After(time.Now()) {
		t.Fatalf("bad login result: user=%+v token=%q expires=%v", user, token, expires)
	}

	got, err := svc.Authenticate(ctx, token)
	if err != nil || got.ID != user.ID {
		t.Fatalf("Authenticate: got %+v err %v", got, err)
	}

	if err := svc.Logout(ctx, token); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if _, err := svc.Authenticate(ctx, token); err != ErrInvalidSession {
		t.Fatalf("Authenticate after logout err = %v, want ErrInvalidSession", err)
	}

	if _, err := svc.Authenticate(ctx, "not-a-real-token"); err != ErrInvalidSession {
		t.Fatalf("Authenticate(garbage) err = %v, want ErrInvalidSession", err)
	}
}
