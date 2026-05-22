package domain

import (
	"errors"
	"strings"
)

// NormalizePhone trims surrounding whitespace from a phone number.
func NormalizePhone(phone string) string {
	return strings.TrimSpace(phone)
}

// ValidatePhone applies light validation — phone is the member key, so it must
// be present and a sane length, but we don't enforce a country-specific format.
func ValidatePhone(phone string) error {
	p := strings.TrimSpace(phone)
	if p == "" {
		return errors.New("phone is required")
	}
	if len(p) < 4 || len(p) > 20 {
		return errors.New("phone must be 4–20 characters")
	}
	return nil
}
