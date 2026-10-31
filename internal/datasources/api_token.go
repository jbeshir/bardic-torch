package datasources

import (
	"context"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// NewAPIToken holds the values an API token is created with. TokenHash is domain.HashAPITokenSecret of the secret.
type NewAPIToken struct {
	ID          string
	UserID      string
	TokenHash   string
	TokenPrefix string
	Name        *string
	CreatedAt   time.Time
}

// APITokenCreator stores a new API token.
type APITokenCreator interface {
	CreateAPIToken(ctx context.Context, token NewAPIToken) error
}

// APITokenByHashGetter retrieves an API token, revoked or not, by domain.HashAPITokenSecret of its secret, returning
// ErrNotFound if there is none.
type APITokenByHashGetter interface {
	GetAPITokenByHash(ctx context.Context, tokenHash string) (domain.APIToken, error)
}

// APITokenLastUsedUpdater records that a token was used at usedAt. To limit writes, the update is skipped
// when the recorded last use is less than a minute before usedAt.
type APITokenLastUsedUpdater interface {
	UpdateAPITokenLastUsed(ctx context.Context, tokenID string, usedAt time.Time) error
}

// ActiveAPITokenLister lists a user's non-revoked tokens, newest first.
type ActiveAPITokenLister interface {
	ListUserActiveAPITokens(ctx context.Context, userID string) ([]domain.APIToken, error)
}

// ActiveAPITokenCounter counts a user's non-revoked tokens.
type ActiveAPITokenCounter interface {
	CountUserActiveAPITokens(ctx context.Context, userID string) (int64, error)
}

// APITokenRevoker revokes one of a user's tokens, returning ErrNotFound if the user has no token with the
// ID. Revoking an already revoked token keeps the original revoked time.
type APITokenRevoker interface {
	RevokeAPIToken(ctx context.Context, userID, tokenID string, revokedAt time.Time) error
}
