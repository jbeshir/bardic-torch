//go:build integration

package mysql_test

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRepository_EnsureUser_SetsPublicID checks that a new user is created with the public ID they are given,
// that an existing user without one, as created by a release that does not set them, is given it, and that a
// public ID another user has is refused, leaving the user without one for the caller to try another.
func TestRepository_EnsureUser_SetsPublicID(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	const wren, ash, rowan = "auth0|wren", "auth0|ash", "auth0|rowan"

	created, err := repo.EnsureUser(ctx, datasources.NewUser{ID: wren, PublicID: "wrenwrenwrenwren",
		DisplayName: "Wren Ensured", CreatedAt: baseTime})
	require.NoError(t, err)
	assert.Equal(t, "wrenwrenwrenwren", created.PublicID)
	assert.Equal(t, "wrenwrenwrenwren", getUser(t, repo, wren).PublicID)

	// A moderator promoted at startup has none until a request or the backfill gives them one.
	require.NoError(t, repo.PromoteToModerator(ctx, datasources.NewUser{ID: rowan, DisplayName: contactModeratorName,
		CreatedAt: baseTime}))
	assert.Empty(t, getUser(t, repo, rowan).PublicID)
	given, err := repo.EnsureUser(ctx, datasources.NewUser{ID: rowan, PublicID: "rowanrowanrowan2",
		DisplayName: "Other", CreatedAt: baseTime})
	require.NoError(t, err)
	assert.Equal(t, domain.User{ID: rowan, PublicID: "rowanrowanrowan2", DisplayName: contactModeratorName,
		Role: domain.RoleModerator, CreatedAt: baseTime}, given)

	_, err = repo.EnsureUser(ctx, datasources.NewUser{ID: ash, PublicID: "wrenwrenwrenwren", DisplayName: "Ash Ensured",
		CreatedAt: baseTime})
	require.ErrorIs(t, err, datasources.ErrPublicIDTaken)
	assert.Empty(t, getUser(t, repo, ash).PublicID, "the user is created, without a public ID")
	retried, err := repo.EnsureUser(ctx, datasources.NewUser{ID: ash, PublicID: "ashashashashash2",
		DisplayName: "Ash Ensured", CreatedAt: baseTime})
	require.NoError(t, err)
	assert.Equal(t, "ashashashashash2", retried.PublicID)

	_, err = repo.EnsureUser(ctx, datasources.NewUser{ID: "auth0|none", DisplayName: "None", CreatedAt: baseTime})
	require.Error(t, err, "a user is never given an empty public ID")
	_, err = repo.GetUser(ctx, "auth0|none")
	require.ErrorIs(t, err, datasources.ErrNotFound)
}

// TestRepository_SetUserPublicIDIfMissing_KeepsExisting checks that a user's public ID, once set, never changes,
// that the users without one are listed oldest first, and that a public ID is case-sensitive and unique.
func TestRepository_SetUserPublicIDIfMissing_KeepsExisting(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	for _, id := range []string{olderUserID, newerUserID} {
		require.NoError(t, repo.PromoteToModerator(ctx, datasources.NewUser{ID: id, DisplayName: id,
			CreatedAt: baseTime}))
	}
	_, err := repo.EnsureUser(ctx, datasources.NewUser{ID: "auth0|set", PublicID: "setsetsetsetset2",
		DisplayName: "Set", CreatedAt: baseTime})
	require.NoError(t, err)

	missing, err := repo.ListUsersWithoutPublicID(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{newerUserID, olderUserID}, missing, "same creation time: by ID")

	require.NoError(t, repo.SetUserPublicIDIfMissing(ctx, "auth0|set", "otherotherother2"))
	assert.Equal(t, "setsetsetsetset2", getUser(t, repo, "auth0|set").PublicID, "an existing public ID is kept")
	require.ErrorIs(t, repo.SetUserPublicIDIfMissing(ctx, olderUserID, "setsetsetsetset2"),
		datasources.ErrPublicIDTaken)
	require.NoError(t, repo.SetUserPublicIDIfMissing(ctx, "auth0|unknown", "unknownunknown22"),
		"no user with the ID: nothing to set")
	require.NoError(t, repo.SetUserPublicIDIfMissing(ctx, olderUserID, "olderolderolder2"))
	assert.Equal(t, "olderolderolder2", getUser(t, repo, olderUserID).PublicID)

	missing, err = repo.ListUsersWithoutPublicID(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{newerUserID}, missing)
}

// The users created at the same time whose missing public IDs are listed by ID.
const (
	olderUserID = "auth0|older"
	newerUserID = "auth0|newer"
)
