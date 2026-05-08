package config

import (
	"testing"
)

func TestLoad(t *testing.T) {
	t.Run("requires PSQL_URL", func(t *testing.T) {
		t.Setenv("PSQL_URL", "")
		_, err := Load()
		if err == nil {
			t.Fatalf("want error when PSQL_URL missing")
		}
	})

	t.Run("env vars unmarshal", func(t *testing.T) {
		t.Setenv("PSQL_URL", "postgres://x")
		t.Setenv("PSQL_DEV_URL", "postgres://dev")
		t.Setenv("PSQL_PROD_URL", "postgres://prod")
		t.Setenv("PORT", "9000")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if cfg.PSQLURL != "postgres://x" {
			t.Errorf("PSQLURL: got %q", cfg.PSQLURL)
		}
		if cfg.PSQLDevURL != "postgres://dev" {
			t.Errorf("PSQLDevURL: got %q", cfg.PSQLDevURL)
		}
		if cfg.PSQLProdURL != "postgres://prod" {
			t.Errorf("PSQLProdURL: got %q", cfg.PSQLProdURL)
		}
		if cfg.Port != "9000" {
			t.Errorf("Port: got %q", cfg.Port)
		}
	})

	t.Run("default port", func(t *testing.T) {
		t.Setenv("PSQL_URL", "postgres://x")
		t.Setenv("PORT", "")

		cfg, err := Load()
		if err != nil {
			t.Fatalf("load: %v", err)
		}
		if cfg.Port != "8080" {
			t.Errorf("Port default: got %q want 8080", cfg.Port)
		}
	})
}
