package domain

import "time"

// User is an account, identified by the subject of the identity provider that authenticated it.
type User struct {
	ID string
	// PublicID addresses the user's public repertoire (IsPublicID); unlike ID it may be shown to anyone. It is
	// empty only for a user created by code that does not set one, until they are given one.
	PublicID    string
	DisplayName string
	Role        Role
	CreatedAt   time.Time
	// BlockedAt is when a moderator blocked the user, or nil if the user is not blocked.
	BlockedAt *time.Time
}

// IsBlocked reports whether the user has been blocked.
func (u User) IsBlocked() bool {
	return u.BlockedAt != nil
}
