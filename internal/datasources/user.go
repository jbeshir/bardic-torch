package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// NewUser holds the values a user is created with if it does not exist yet. PublicID is the public ID a user
// without one is given.
type NewUser struct {
	ID          string
	PublicID    string
	DisplayName string
	CreatedAt   time.Time
}

// UserGetter retrieves a user by ID, returning ErrNotFound if there is none.
type UserGetter interface {
	GetUser(ctx context.Context, userID string) (domain.User, error)
}

// UserEnsurer creates a contributor from user unless a user with its ID already exists, and returns the
// stored user. A new user's display name key is stored too, unless another user has it. A user without a public
// ID is given user.PublicID, and ErrPublicIDTaken is returned if another user has it; an existing user is otherwise
// returned unchanged.
type UserEnsurer interface {
	EnsureUser(ctx context.Context, user NewUser) (domain.User, error)
}

// ModeratorPromoter makes a user a moderator, creating it from user if it does not exist yet. An existing
// user keeps its display name and is unblocked, because moderators cannot be blocked.
type ModeratorPromoter interface {
	PromoteToModerator(ctx context.Context, user NewUser) error
}

// DisplayNameUpdater sets a user's display name and its key (domain.DisplayNameKey), returning ErrNotFound if
// there is no such user and ErrDisplayNameTaken if another user's display name has the same key.
type DisplayNameUpdater interface {
	UpdateDisplayName(ctx context.Context, userID, displayName string) error
}

// UserBlocker blocks a contributor, returning ErrNotFound if there is no contributor with the ID. Blocking
// an already blocked contributor keeps the original blocked time.
type UserBlocker interface {
	BlockUser(ctx context.Context, userID string, blockedAt time.Time) error
}

// UserUnblocker unblocks a contributor, returning ErrNotFound if there is no contributor with the ID.
type UserUnblocker interface {
	UnblockUser(ctx context.Context, userID string) error
}

// ErrDisplayNameTaken is returned, wrapped, when another user's display name has the same domain.DisplayNameKey.
var ErrDisplayNameTaken = errors.New("display name taken")

// UserDisplayNameKey is a user's display name with the key stored for it: nil when none is stored.
type UserDisplayNameKey struct {
	UserID      string
	DisplayName string
	Key         *string
}

// DisplayNameKeyLister lists every user's display name and stored key, oldest user first, then by ID.
type DisplayNameKeyLister interface {
	ListDisplayNameKeys(ctx context.Context) ([]UserDisplayNameKey, error)
}

// DisplayNameKeySetter stores key as a user's display name key, nil for none, returning ErrNotFound if there is no
// such user and ErrDisplayNameTaken if another user has the key.
type DisplayNameKeySetter interface {
	SetDisplayNameKey(ctx context.Context, userID string, key *string) error
}

// ErrPublicIDTaken is returned, wrapped, when another user already has the public ID a user is to be given.
var ErrPublicIDTaken = errors.New("public ID taken")

// PublicIDSetter gives a user the public ID publicID unless they already have one, returning ErrPublicIDTaken if
// another user has it. A user who has one, or no user with the ID, is left as is.
type PublicIDSetter interface {
	SetUserPublicIDIfMissing(ctx context.Context, userID, publicID string) error
}

// UsersWithoutPublicIDLister lists the IDs of the users without a public ID, oldest user first, then by ID.
type UsersWithoutPublicIDLister interface {
	ListUsersWithoutPublicID(ctx context.Context) ([]string, error)
}
