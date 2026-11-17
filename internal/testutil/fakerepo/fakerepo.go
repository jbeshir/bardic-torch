// Package fakerepo is the one hand-written fake of the datasources for tests above the database: it holds users
// in a map and returns canned values. Tests of the real queries use MySQL instead.
package fakerepo

import (
	"context"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.Repository = (*Repo)(nil)
)

// When Err is set every method returns it
// instead. A Repo is not safe for concurrent use: tests make one each.
type Repo struct {
	// Users are the stored users by ID.
	Users map[string]domain.User

	// Err, if set, is returned by every method, and PingErr by Ping only.
	Err     error
	PingErr error
}

func (r *Repo) Ping(context.Context) error {
	if r.Err != nil {
		return r.Err
	}
	return r.PingErr
}

func (r *Repo) GetUser(_ context.Context, userID string) (domain.User, error) {
	user, ok := r.Users[userID]
	if r.Err != nil || !ok {
		return domain.User{}, notFoundOr(r.Err)
	}
	return user, nil
}

func (r *Repo) EnsureUser(_ context.Context, user datasources.NewUser) (domain.User, error) {
	if r.Err != nil {
		return domain.User{}, r.Err
	}
	if stored, ok := r.Users[user.ID]; ok {
		return stored, nil
	}
	stored := domain.User{ID: user.ID, DisplayName: user.DisplayName, Role: domain.RoleContributor,
		CreatedAt: user.CreatedAt}
	r.put(stored)
	return stored, nil
}

func (r *Repo) PromoteToModerator(_ context.Context, user datasources.NewUser) error {
	if r.Err != nil {
		return r.Err
	}
	stored, ok := r.Users[user.ID]
	if !ok {
		stored = domain.User{ID: user.ID, DisplayName: user.DisplayName, CreatedAt: user.CreatedAt}
	}
	stored.Role, stored.BlockedAt = domain.RoleModerator, nil
	r.put(stored)
	return nil
}

func (r *Repo) UpdateDisplayName(_ context.Context, userID, displayName string) error {
	stored, ok := r.Users[userID]
	if r.Err != nil || !ok {
		return notFoundOr(r.Err)
	}
	stored.DisplayName = displayName
	r.put(stored)
	return nil
}

func (r *Repo) BlockUser(_ context.Context, userID string, blockedAt time.Time) error {
	return r.setBlocked(userID, &blockedAt)
}

func (r *Repo) UnblockUser(_ context.Context, userID string) error {
	return r.setBlocked(userID, nil)
}

// setBlocked blocks or unblocks a contributor, as the real update matches only contributors.
func (r *Repo) setBlocked(userID string, blockedAt *time.Time) error {
	stored, ok := r.Users[userID]
	if r.Err != nil || !ok || stored.Role != domain.RoleContributor {
		return notFoundOr(r.Err)
	}
	if blockedAt == nil || stored.BlockedAt == nil {
		stored.BlockedAt = blockedAt
	}
	r.put(stored)
	return nil
}

func (r *Repo) put(user domain.User) {
	if r.Users == nil {
		r.Users = map[string]domain.User{}
	}
	r.Users[user.ID] = user
}

// notFoundOr is err, or datasources.ErrNotFound if err is nil.
func notFoundOr(err error) error {
	if err != nil {
		return err
	}
	return datasources.ErrNotFound
}
