package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
)

// paymentConfig is the POS-local, on-disk configuration for the checkout
// payment channels: which of cash/card/QR the cashier may pick, and which one
// is pre-selected when the tender modal opens. It lives in a JSON file next to
// the agent (not browser localStorage) so it survives agent restarts and
// browser-data clears, and every browser pointed at this terminal's agent sees
// the same setting.
type paymentConfig struct {
	Cash    bool   `json:"cash"`
	Card    bool   `json:"card"`
	QR      bool   `json:"qr"`
	Default string `json:"default"` // "cash" | "card" | "qr"; must be enabled
}

func defaultPaymentConfig() paymentConfig {
	return paymentConfig{Cash: true, Card: true, QR: true, Default: "cash"}
}

// enabled reports whether the named channel is currently offered.
func (c paymentConfig) enabled(method string) bool {
	switch method {
	case "cash":
		return c.Cash
	case "card":
		return c.Card
	case "qr":
		return c.QR
	}
	return false
}

// validate rejects a config that would leave the POS unable to take payment (no
// channel enabled) or with a default pointing at a disabled/unknown channel.
func (c paymentConfig) validate() error {
	if !c.Cash && !c.Card && !c.QR {
		return fmt.Errorf("at least one payment channel must be enabled")
	}
	switch c.Default {
	case "cash", "card", "qr":
	default:
		return fmt.Errorf("default must be one of cash, card, qr")
	}
	if !c.enabled(c.Default) {
		return fmt.Errorf("default channel %q is not enabled", c.Default)
	}
	return nil
}

// paymentConfigStore persists a paymentConfig as JSON on disk, guarded by a
// mutex so concurrent reads/writes from multiple browser tabs stay consistent.
type paymentConfigStore struct {
	mu   sync.Mutex
	path string
}

func newPaymentConfigStore(path string) *paymentConfigStore {
	return &paymentConfigStore{path: path}
}

// load returns the persisted config, or defaults when the file does not yet
// exist (first run — the file is created on the first save). A malformed file
// is surfaced as an error rather than silently reset.
func (s *paymentConfigStore) load() (paymentConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return defaultPaymentConfig(), nil
	}
	if err != nil {
		return paymentConfig{}, err
	}
	var c paymentConfig
	if err := json.Unmarshal(b, &c); err != nil {
		return paymentConfig{}, err
	}
	return c, nil
}

// save validates then atomically writes the config (temp file + rename) so a
// crash mid-write cannot leave a truncated JSON file behind.
func (s *paymentConfigStore) save(c paymentConfig) error {
	if err := c.validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path) // Go's os.Rename replaces on Windows too
}

// getPaymentConfigHandler serves the current payment-channel config to the POS
// on load. Returns the config object directly (no envelope) — agent-local
// endpoints don't use the API's data wrapper.
func getPaymentConfigHandler(store *paymentConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := store.load()
		if err != nil {
			log.Printf("payment config load error: %v", err)
			http.Error(w, "failed to read payment config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(c)
	}
}

// putPaymentConfigHandler persists a new payment-channel config from the POS
// Settings dialog. Invalid configs (all channels off, bad default) → 400.
func putPaymentConfigHandler(store *paymentConfigStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var c paymentConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		if err := c.validate(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := store.save(c); err != nil {
			log.Printf("payment config save error: %v", err)
			http.Error(w, "failed to save payment config", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(c)
	}
}
