package domain

import "time"

// APIToken is a long-lived credential a user created for programmatic access. The secret itself is never
// stored; Prefix is its first characters, kept so the user can tell tokens apart.
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
