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

// GetUser retrieves a user by ID, returning datasources.ErrNotFound if there is none.
func (r *Repository) GetUser(ctx context.Context, userID string) (domain.User, error) {
	row, err := r.queries.GetUser(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, fmt.Errorf("user [%s]: %w", userID, datasources.ErrNotFound)
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("fetching user [%s]: %w", userID, unavailableIfDisconnected(err))
	}

	return convertUser(row)
}

// EnsureUser creates a contributor from user unless a user with its ID already exists, and returns the
// stored user. Concurrent calls for the same ID create one user.
func (r *Repository) EnsureUser(ctx context.Context, user datasources.NewUser) (domain.User, error) {
	err := r.queries.InsertUserIfMissing(ctx, queries.InsertUserIfMissingParams{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		CreatedAt:   user.CreatedAt,
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("inserting user [%s]: %w", user.ID, unavailableIfDisconnected(err))
	}

	return r.GetUser(ctx, user.ID)
}

// PromoteToModerator makes a user a moderator and unblocks it, creating it from user if it does not exist yet.
func (r *Repository) PromoteToModerator(ctx context.Context, user datasources.NewUser) error {
	err := r.queries.PromoteToModerator(ctx, queries.PromoteToModeratorParams{
		ID:          user.ID,
		DisplayName: user.DisplayName,
		CreatedAt:   user.CreatedAt,
	})
	if err != nil {
		return fmt.Errorf("promoting user [%s] to moderator: %w", user.ID, err)
	}

	return nil
}

// UpdateDisplayName sets a user's display name, returning datasources.ErrNotFound if there is no such user.
func (r *Repository) UpdateDisplayName(ctx context.Context, userID, displayName string) error {
	err := requireMatched(r.queries.UpdateDisplayName(ctx, queries.UpdateDisplayNameParams{
		DisplayName: displayName,
		ID:          userID,
	}))
	if err != nil {
		return fmt.Errorf("updating display name of user [%s]: %w", userID, err)
	}

	return nil
}

// BlockUser blocks a contributor, keeping the original time if it is already blocked. It returns
// datasources.ErrNotFound if there is no contributor with the ID.
func (r *Repository) BlockUser(ctx context.Context, userID string, blockedAt time.Time) error {
	err := requireMatched(r.queries.BlockUser(ctx, queries.BlockUserParams{
		BlockedAt: nullTime(blockedAt),
		ID:        userID,
	}))
	if err != nil {
		return fmt.Errorf("blocking contributor [%s]: %w", userID, err)
	}

	return nil
}

// UnblockUser unblocks a contributor, returning datasources.ErrNotFound if there is no contributor with the
// ID.
func (r *Repository) UnblockUser(ctx context.Context, userID string) error {
	if err := requireMatched(r.queries.UnblockUser(ctx, userID)); err != nil {
		return fmt.Errorf("unblocking contributor [%s]: %w", userID, err)
	}

	return nil
}

func convertUser(row queries.User) (domain.User, error) {
	role, err := domain.ParseRole(string(row.Role))
	if err != nil {
		return domain.User{}, fmt.Errorf("user [%s]: %w", row.ID, err)
	}

	return domain.User{
		ID:          row.ID,
		DisplayName: row.DisplayName,
		Role:        role,
		CreatedAt:   row.CreatedAt,
		BlockedAt:   timePtr(row.BlockedAt),
	}, nil
}
