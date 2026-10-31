package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// APITokenPrefix starts every API token secret, so that the Authorization header tells API tokens apart from
// other credentials.
const APITokenPrefix = "user_api|"

// HashAPITokenSecret returns the hex SHA-256 of the whole secret, prefix included. The hash is stored in place
// of the secret, and tokens are looked up by it.
func HashAPITokenSecret(secret string) string {
	hash := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(hash[:])
}

// APIToken is a long-lived credential a user created for programmatic access. The secret itself is never
// stored; Prefix is the first characters of its random part, kept so the user can tell tokens apart.
type APIToken struct {
	ID     string
	UserID string
	Prefix string
	// Name is the label the user gave the token, or nil if they gave none.
	Name      *string
	CreatedAt time.Time
	// LastUsedAt is when the token last authenticated a request, or nil if it never has.
	LastUsedAt *time.Time
	// RevokedAt is when the token was revoked, or nil if it is still active.
	RevokedAt *time.Time
}

// IsActive reports whether the token can still authenticate requests.
func (t APIToken) IsActive() bool {
	return t.RevokedAt == nil
}
