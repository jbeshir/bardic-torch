package datasources

import (
	"context"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// NewUser holds the values a user is created with if it does not exist yet.
type NewUser struct {
	ID          string
	DisplayName string
	CreatedAt   time.Time
}

// Users stores the users.
type Users interface {
	// GetUser retrieves a user by ID, returning ErrNotFound if there is none.
	GetUser(ctx context.Context, userID string) (domain.User, error)
	// EnsureUser creates a contributor from user unless a user with its ID already exists, and returns the
	// stored user. An existing user is returned unchanged.
	EnsureUser(ctx context.Context, user NewUser) (domain.User, error)
	// PromoteToModerator makes a user a moderator, creating it from user if it does not exist yet. An existing
	// user keeps its display name and is unblocked, because moderators cannot be blocked.
	PromoteToModerator(ctx context.Context, user NewUser) error
	// UpdateDisplayName sets a user's display name, returning ErrNotFound if there is no such user.
	UpdateDisplayName(ctx context.Context, userID, displayName string) error
	// BlockUser blocks a contributor, returning ErrNotFound if there is no contributor with the ID. Blocking an
	// already blocked contributor keeps the original blocked time.
	BlockUser(ctx context.Context, userID string, blockedAt time.Time) error
	// UnblockUser unblocks a contributor, returning ErrNotFound if there is no contributor with the ID.
	UnblockUser(ctx context.Context, userID string) error
}
