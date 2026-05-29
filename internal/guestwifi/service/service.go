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
	Host         string // e.g. "192.168.1.39"
	Port         int    // 8728
	User         string
	Password     string
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
	_, err = c.RunArgs([]string{
		"/ip/hotspot/user/add",
		"=name=" + username,
		"=password=",
		"=server=" + s.cfg.HotspotServer,
		"=disabled=yes",
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

// EnableForOrder enables the MikroTik hotspot user assigned to this order.
// Called at checkout (after payment confirmed, inside or just after the tx).
func (s *Service) EnableForOrder(ctx context.Context, orderID int32) error {
	row, err := s.q.GetAssignedWifiUserByOrder(ctx, pgtype.Int4{Int32: orderID, Valid: true})
	if err != nil {
		return nil // order had no wifi user — ok
	}
	if err := s.mikrotikSetDisabled(row.Username, false); err != nil {
		return fmt.Errorf("enable mikrotik user %q: %w", row.Username, err)
	}
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
