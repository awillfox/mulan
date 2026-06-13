package domain

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// Roles. owner has every permission; staff is the default reduced role.
const (
	RoleOwner = "owner"
	RoleStaff = "staff"
)

// ValidRole reports whether r is a recognised manager role.
func ValidRole(r string) bool {
	return r == RoleOwner || r == RoleStaff
}

// User is the authenticated manager identity carried through the request.
type User struct {
	ID       int32  `json:"id"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

// GenerateToken returns a fresh, URL-safe opaque session token (32 random
// bytes, base64url, no padding). This is the value handed to the client; only
// its hash is stored server-side.
func GenerateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the hex SHA-256 of a token. Deterministic: the same token
// always hashes to the same 64-char string, so we can look sessions up by hash
// without ever persisting the raw token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
