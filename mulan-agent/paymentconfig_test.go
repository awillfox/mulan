package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateAcceptsPromptPayIDs(t *testing.T) {
	base := paymentConfig{Cash: true, Card: true, QR: true, Default: "cash"}
	for _, id := range []string{"", "0923979957", "092-397-9957", "1234567890123", "123456789012345"} {
		c := base
		c.PromptPayID = id
		if err := c.validate(); err != nil {
			t.Errorf("validate(%q) = %v, want nil", id, err)
		}
	}
}

func TestValidateRejectsBadPromptPayID(t *testing.T) {
	base := paymentConfig{Cash: true, Card: true, QR: true, Default: "cash"}
	for _, id := range []string{"123", "abcdefghij", "12345678901234567890"} {
		c := base
		c.PromptPayID = id
		if err := c.validate(); err == nil {
			t.Errorf("validate(%q) = nil, want error", id)
		}
	}
}

// TestLoadOldConfigFileWithoutPromptPay covers the deployed pos-config.json on
// the POS terminal, which was written before this field existed: it must load
// cleanly with an empty id rather than erroring.
func TestLoadOldConfigFileWithoutPromptPay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pos-config.json")
	old := `{"cash":true,"card":false,"qr":true,"default":"qr"}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c, err := newPaymentConfigStore(path).load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.PromptPayID != "" {
		t.Errorf("PromptPayID = %q, want empty", c.PromptPayID)
	}
	if !c.QR || c.Card || c.Default != "qr" {
		t.Errorf("existing fields not preserved: %+v", c)
	}
}

func TestSaveRoundTripsPromptPayID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pos-config.json")
	s := newPaymentConfigStore(path)
	in := paymentConfig{Cash: true, Card: true, QR: true, Default: "qr", PromptPayID: "0923979957"}
	if err := s.save(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := s.load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out != in {
		t.Errorf("round trip: got %+v, want %+v", out, in)
	}
}
