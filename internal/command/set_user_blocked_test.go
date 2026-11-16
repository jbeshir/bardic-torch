package command

import (
	"context"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func userWithRole(role domain.Role) domain.User {
	return domain.User{ID: resolvedUserID, DisplayName: testDisplayName, Role: role, CreatedAt: fixedNow}
}

// missesFirstUpdates is a repository whose first misses block or unblock attempts find no contributor, though
// the user is stored: as when the user's first request creates them between the update and the read.
type missesFirstUpdates struct {
	*fakerepo.Repo
	misses int
}

func (r *missesFirstUpdates) BlockUser(ctx context.Context, userID string, blockedAt time.Time) error {
	if r.misses > 0 {
		r.misses--
		return datasources.ErrNotFound
	}
	return r.Repo.BlockUser(ctx, userID, blockedAt)
}

func (r *missesFirstUpdates) UnblockUser(ctx context.Context, userID string) error {
	if r.misses > 0 {
		r.misses--
		return datasources.ErrNotFound
	}
	return r.Repo.UnblockUser(ctx, userID)
}

func TestSetUserBlocked_ModeratorIsNotBlockable(t *testing.T) {
	t.Parallel()

	for _, blocked := range []bool{true, false} {
		t.Run(blockedName(blocked), func(t *testing.T) {
			t.Parallel()

			repo := &fakerepo.Repo{Users: map[string]domain.User{resolvedUserID: userWithRole(domain.RoleModerator)}}
			cmd := &SetUserBlocked{Users: repo, Now: fixedClock}

			_, err := cmd.Execute(t.Context(), SetUserBlockedRequest{UserID: resolvedUserID, Blocked: blocked})

			require.ErrorIs(t, err, ErrModeratorNotBlockable)
		})
	}
}

func TestSetUserBlocked_RetriesContributorCreatedDuringUpdate(t *testing.T) {
	t.Parallel()

	for _, blocked := range []bool{true, false} {
		t.Run(blockedName(blocked), func(t *testing.T) {
			t.Parallel()

			repo := &missesFirstUpdates{
				Repo:   &fakerepo.Repo{Users: map[string]domain.User{resolvedUserID: userWithRole(domain.RoleContributor)}},
				misses: 1,
			}
			cmd := &SetUserBlocked{Users: repo, Now: fixedClock}

			user, err := cmd.Execute(t.Context(), SetUserBlockedRequest{UserID: resolvedUserID, Blocked: blocked})

			require.NoError(t, err)
			assert.Equal(t, blocked, user.BlockedAt != nil)
		})
	}
}

func TestSetUserBlocked_ContributorNeverMatchingFails(t *testing.T) {
	t.Parallel()

	repo := &missesFirstUpdates{
		Repo:   &fakerepo.Repo{Users: map[string]domain.User{resolvedUserID: userWithRole(domain.RoleContributor)}},
		misses: 2,
	}
	cmd := &SetUserBlocked{Users: repo, Now: fixedClock}

	_, err := cmd.Execute(t.Context(), SetUserBlockedRequest{UserID: resolvedUserID, Blocked: true})

	require.ErrorIs(t, err, errContributorNotMatched)
	require.NotErrorIs(t, err, ErrModeratorNotBlockable)
}

func blockedName(blocked bool) string {
	if blocked {
		return "block"
	}
	return "unblock"
}
