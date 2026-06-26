package service

import (
	"context"
	"crypto/rand"
	"fmt"
	"log"
	"mulan/sqlc"
	"time"

	routeros "github.com/go-routeros/routeros/v3"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	poolTarget    = 10
	refillTrigger = 3
)

type Config struct {
	Host          string // e.g. "192.168.1.39"
	Port          int    // 8728
	User          string
	Password      string
	HotspotServer string
}

type Service struct {
	db  *pgxpool.Pool
	q   *sqlc.Queries
	cfg Config
}

func New(db *pgxpool.Pool, cfg Config) *Service {
	return &Service{db: db, q: sqlc.New(db), cfg: cfg}
}

// ─── MikroTik connection ────────────────────────────────────────────────────

func (s *Service) dial() (*routeros.Client, error) {
	addr := fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port)
	return routeros.DialTimeout(addr, s.cfg.User, s.cfg.Password, 10*time.Second)
}

// ─── Username generation ────────────────────────────────────────────────────

func randomUsername(ctx context.Context, q *sqlc.Queries) (string, error) {
	adjectives := []string{"red", "blue", "sun", "sea", "sky", "gold", "oak", "bay", "zen", "fox"}
	nouns := []string{"cat", "dog", "fish", "bird", "lion", "bear", "frog", "wolf", "crow", "deer"}
	for range 20 {
		adj := adjectives[randByte()%uint8(len(adjectives))]
		noun := nouns[randByte()%uint8(len(nouns))]
		n := randByte()%90 + 10 // 10–99
		name := fmt.Sprintf("%s%s%d", adj, noun, n)
		count, err := q.UsernameExists(ctx, name)
		if err != nil {
			return "", err
		}
		if count == 0 {
			return name, nil
		}
	}
	return "", fmt.Errorf("could not generate unique username after 20 attempts")
}

func randByte() uint8 {
	b := make([]byte, 1)
	_, _ = rand.Read(b)
	return b[0]
}

// ─── MikroTik operations ────────────────────────────────────────────────────

func (s *Service) mikrotikCreate(username string) error {
	c, err := s.dial()
	if err != nil {
		return fmt.Errorf("mikrotik dial: %w", err)
	}
	defer c.Close()
	// Pool users are created already enabled so that checkout never depends on a
	// live MikroTik API call to make a printed voucher work. The expire loop and
	// reconcile loop disable accounts once their order is done.
	_, err = c.RunArgs([]string{
		"/ip/hotspot/user/add",
		"=name=" + username,
		"=password=",
		"=server=" + s.cfg.HotspotServer,
		"=disabled=no",
	})
	return err
}

func (s *Service) mikrotikSetDisabled(username string, disabled bool) error {
	c, err := s.dial()
	if err != nil {
		return fmt.Errorf("mikrotik dial: %w", err)
	}
	defer c.Close()

	reply, err := c.RunArgs([]string{"/ip/hotspot/user/print", "?name=" + username, "=.proplist=.id"})
	if err != nil {
		return fmt.Errorf("mikrotik find user: %w", err)
	}
	if len(reply.Re) == 0 {
		return fmt.Errorf("mikrotik user %q not found", username)
	}
	id := reply.Re[0].Map[".id"]

	val := "yes"
	if !disabled {
		val = "no"
	}
	_, err = c.RunArgs([]string{"/ip/hotspot/user/set", "=.id=" + id, "=disabled=" + val})
	return err
}

// ─── Pool management ────────────────────────────────────────────────────────

// FillPool creates disabled MikroTik accounts until pool reaches poolTarget.
func (s *Service) FillPool(ctx context.Context) error {
	pending, err := s.q.CountPendingGuestWifiUsers(ctx)
	if err != nil {
		return err
	}
	need := int64(poolTarget) - pending
	if need <= 0 {
		return nil
	}
	for range need {
		username, err := randomUsername(ctx, s.q)
		if err != nil {
			return err
		}
		if err := s.mikrotikCreate(username); err != nil {
			return fmt.Errorf("create mikrotik user %q: %w", username, err)
		}
		if _, err := s.q.CreateGuestWifiUser(ctx, username); err != nil {
			return fmt.Errorf("insert wifi user %q: %w", username, err)
		}
		log.Printf("guestwifi: created %q", username)
	}
	return nil
}

// AssignToOrder picks a pending user and marks it assigned to the order.
// Returns "" if no pending users are available (caller should still proceed).
func (s *Service) AssignToOrder(ctx context.Context, orderID int32) (string, error) {
	pending, err := s.q.GetPendingWifiUser(ctx)
	if err != nil {
		return "", nil // no pending — non-fatal
	}
	assigned, err := s.q.AssignGuestWifiUser(ctx, sqlc.AssignGuestWifiUserParams{
		OrderID: pgtype.Int4{Int32: orderID, Valid: true},
		ID:      pending.ID,
	})
	if err != nil {
		return "", err
	}

	// Refill async if below trigger
	go func() {
		bg := context.Background()
		cnt, err := s.q.CountPendingGuestWifiUsers(bg)
		if err == nil && cnt <= refillTrigger {
			if err := s.FillPool(bg); err != nil {
				log.Printf("guestwifi: refill error: %v", err)
			}
		}
	}()

	return assigned.Username, nil
}

// EnableForOrder marks this order's assigned hotspot user active (DB only).
// Called at checkout. The MikroTik account is already enabled (created enabled
// in FillPool), so checkout deliberately does NOT make a live MikroTik API call
// here — a transient API outage must never produce a printed-but-dead voucher.
// The reconcile loop is the backstop that keeps MikroTik in sync with the DB.
func (s *Service) EnableForOrder(ctx context.Context, orderID int32) error {
	return s.q.ActivateGuestWifiUser(ctx, pgtype.Int4{Int32: orderID, Valid: true})
}

// GetUsernameForOrder returns the wifi username assigned to an order (if any).
func (s *Service) GetUsernameForOrder(ctx context.Context, orderID int32) string {
	row, err := s.q.GetAssignedWifiUserByOrder(ctx, pgtype.Int4{Int32: orderID, Valid: true})
	if err != nil {
		return ""
	}
	return row.Username
}

// ExpireLoop runs a background goroutine that disables expired accounts every minute.
func (s *Service) ExpireLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.expireOnce()
			}
		}
	}()
}

func (s *Service) expireOnce() {
	ctx := context.Background()
	expired, err := s.q.ExpireGuestWifiUsers(ctx)
	if err != nil {
		log.Printf("guestwifi: expire query error: %v", err)
		return
	}
	for _, u := range expired {
		if err := s.mikrotikSetDisabled(u.Username, true); err != nil {
			log.Printf("guestwifi: disable mikrotik %q: %v", u.Username, err)
		} else {
			log.Printf("guestwifi: expired %q", u.Username)
		}
	}
}

// ReconcileLoop periodically forces MikroTik state to match the DB, healing any
// drift left by transient API failures (a failed enable at checkout, a failed
// disable at expiry). It runs every 2 minutes and once immediately.
func (s *Service) ReconcileLoop(ctx context.Context) {
	go func() {
		if err := s.Reconcile(ctx); err != nil {
			log.Printf("guestwifi: reconcile error: %v", err)
		}
		ticker := time.NewTicker(2 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.Reconcile(ctx); err != nil {
					log.Printf("guestwifi: reconcile error: %v", err)
				}
			}
		}
	}()
}

// Reconcile makes MikroTik match the DB on a single connection:
//   - DB state pending/assigned/active  → user enabled  (recreated if missing)
//   - DB state expired                  → user disabled
//
// MikroTik users not present in the DB (e.g. the built-in "guest"/trial account)
// are never touched.
func (s *Service) Reconcile(ctx context.Context) error {
	users, err := s.q.ListGuestWifiUsers(ctx)
	if err != nil {
		return fmt.Errorf("reconcile list db: %w", err)
	}

	c, err := s.dial()
	if err != nil {
		return fmt.Errorf("reconcile dial: %w", err)
	}
	defer c.Close()

	reply, err := c.RunArgs([]string{"/ip/hotspot/user/print", "=.proplist=.id,name,disabled"})
	if err != nil {
		return fmt.Errorf("reconcile list mikrotik: %w", err)
	}
	type mtUser struct {
		id       string
		disabled bool
	}
	existing := make(map[string]mtUser, len(reply.Re))
	for _, re := range reply.Re {
		d := re.Map["disabled"]
		existing[re.Map["name"]] = mtUser{id: re.Map[".id"], disabled: d == "true" || d == "yes"}
	}

	var enabled, disabled, created, errs int
	for _, u := range users {
		wantDisabled := u.State == "expired"
		mt, ok := existing[u.Username]
		if !ok {
			if wantDisabled {
				continue // expired and already gone from MikroTik — nothing to do
			}
			if err := s.mikrotikCreate(u.Username); err != nil {
				errs++
				log.Printf("guestwifi: reconcile recreate %q: %v", u.Username, err)
			} else {
				created++
			}
			continue
		}
		if mt.disabled == wantDisabled {
			continue
		}
		val := "no"
		if wantDisabled {
			val = "yes"
		}
		if _, err := c.RunArgs([]string{"/ip/hotspot/user/set", "=.id=" + mt.id, "=disabled=" + val}); err != nil {
			errs++
			log.Printf("guestwifi: reconcile set %q disabled=%s: %v", u.Username, val, err)
			continue
		}
		if wantDisabled {
			disabled++
		} else {
			enabled++
		}
	}
	if enabled+disabled+created+errs > 0 {
		log.Printf("guestwifi: reconcile enabled=%d disabled=%d created=%d errs=%d", enabled, disabled, created, errs)
	}
	return nil
}
