package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// apiTokenLastUsedResolution is how stale a token's recorded last use must be before it is updated again.
const apiTokenLastUsedResolution = time.Minute

var (
	_ datasources.APITokenCreator         = (*Repository)(nil)
	_ datasources.APITokenByHashGetter    = (*Repository)(nil)
	_ datasources.APITokenLastUsedUpdater = (*Repository)(nil)
	_ datasources.ActiveAPITokenLister    = (*Repository)(nil)
	_ datasources.ActiveAPITokenCounter   = (*Repository)(nil)
	_ datasources.APITokenRevoker         = (*Repository)(nil)
)

// CreateAPIToken stores a new API token.
func (r *Repository) CreateAPIToken(ctx context.Context, token datasources.NewAPIToken) error {
	var name sql.NullString
	if token.Name != nil {
		name = sql.NullString{String: *token.Name, Valid: true}
	}

	err := r.queries.CreateAPIToken(ctx, queries.CreateAPITokenParams{
		ID:          token.ID,
		UserID:      token.UserID,
		TokenHash:   token.TokenHash,
		TokenPrefix: token.TokenPrefix,
		Name:        name,
		CreatedAt:   token.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("creating API token for user [%s]: %w", token.UserID, err)
	}

	return nil
}

// GetAPITokenByHash retrieves an API token, revoked or not, by the hex SHA-256 of its secret, returning
// datasources.ErrNotFound if there is none.
func (r *Repository) GetAPITokenByHash(ctx context.Context, tokenHash string) (domain.APIToken, error) {
	row, err := r.queries.GetAPITokenByHash(ctx, tokenHash)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.APIToken{}, fmt.Errorf("API token: %w", datasources.ErrNotFound)
	}
	if err != nil {
		return domain.APIToken{}, fmt.Errorf("fetching API token by hash: %w", err)
	}

	return convertAPIToken(row), nil
}

// UpdateAPITokenLastUsed records that a token was used at usedAt, unless its recorded last use is less than
// apiTokenLastUsedResolution before usedAt.
func (r *Repository) UpdateAPITokenLastUsed(ctx context.Context, tokenID string, usedAt time.Time) error {
	err := r.queries.UpdateAPITokenLastUsed(ctx, queries.UpdateAPITokenLastUsedParams{
		UsedAt:      nullTime(usedAt),
		ID:          tokenID,
		StaleBefore: nullTime(usedAt.Add(-apiTokenLastUsedResolution)),
	})
	if err != nil {
		return fmt.Errorf("updating last use of API token [%s]: %w", tokenID, err)
	}

	return nil
}

// ListUserActiveAPITokens lists a user's non-revoked tokens, newest first.
func (r *Repository) ListUserActiveAPITokens(ctx context.Context, userID string) ([]domain.APIToken, error) {
	rows, err := r.queries.ListUserActiveAPITokens(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("listing API tokens of user [%s]: %w", userID, err)
	}

	tokens := make([]domain.APIToken, 0, len(rows))
	for _, row := range rows {
		tokens = append(tokens, convertAPIToken(row))
	}

	return tokens, nil
}

// CountUserActiveAPITokens counts a user's non-revoked tokens.
func (r *Repository) CountUserActiveAPITokens(ctx context.Context, userID string) (int64, error) {
	count, err := r.queries.CountUserActiveAPITokens(ctx, userID)
	if err != nil {
		return 0, fmt.Errorf("counting API tokens of user [%s]: %w", userID, err)
	}

	return count, nil
}

// RevokeAPIToken revokes one of a user's tokens, keeping the original time if it is already revoked. It
// returns datasources.ErrNotFound if the user has no token with the ID.
func (r *Repository) RevokeAPIToken(ctx context.Context, userID, tokenID string, revokedAt time.Time) error {
	err := requireMatched(r.queries.RevokeAPIToken(ctx, queries.RevokeAPITokenParams{
		RevokedAt: nullTime(revokedAt),
		ID:        tokenID,
		UserID:    userID,
	}))
	if err != nil {
		return fmt.Errorf("revoking API token [%s] of user [%s]: %w", tokenID, userID, err)
	}

	return nil
}

func convertAPIToken(row queries.ApiToken) domain.APIToken {
	return domain.APIToken{
		ID:         row.ID,
		UserID:     row.UserID,
		Prefix:     row.TokenPrefix,
		Name:       stringPtr(row.Name),
		CreatedAt:  row.CreatedAt,
		LastUsedAt: timePtr(row.LastUsedAt),
		RevokedAt:  timePtr(row.RevokedAt),
	}
}
