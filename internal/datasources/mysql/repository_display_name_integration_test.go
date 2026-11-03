//go:build integration

package mysql_test

import (
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// displayNameKeys returns every user's stored display name key by user ID, nil for none.
func displayNameKeys(t *testing.T, repo *mysql.Repository) map[string]*string {
	t.Helper()
	keys, err := repo.ListDisplayNameKeys(t.Context())
	require.NoError(t, err)
	byID := map[string]*string{}
	for _, k := range keys {
		byID[k.UserID] = k.Key
	}

	return byID
}

// TestRepository_UpdateDisplayName_KeyUnique checks that display names are unique by their key on one database:
// "Morwen", "morwen" and "Mor-wen" are one name, a user may keep their own, a freed key may be taken, a name whose
// key is longer than the name is stored, a name without letters or digits has no key, and a new user whose name
// another user's key holds is created without a key.
func TestRepository_UpdateDisplayName_KeyUnique(t *testing.T) {
	t.Parallel()
	repo := newRepository(t)
	const first, second, third = "auth0|first", "auth0|second", "auth0|third"
	for _, id := range []string{first, second} {
		ensureUser(t, repo, id, "Bard "+strings.TrimPrefix(id, "auth0|"))
	}
	ctx := t.Context()

	require.NoError(t, repo.UpdateDisplayName(ctx, first, "Morwen"))
	for _, name := range []string{"Morwen", "morwen", "Mor-wen", "MOR WEN"} {
		require.ErrorIs(t, repo.UpdateDisplayName(ctx, second, name), datasources.ErrDisplayNameTaken, name)
	}
	assert.Equal(t, "Bard second", getUser(t, repo, second).DisplayName, "a refused rename stores nothing")
	require.NoError(t, repo.UpdateDisplayName(ctx, first, "MORWEN"), "a user may respell their own name")
	assert.Equal(t, new("morwen"), displayNameKeys(t, repo)[first])

	require.NoError(t, repo.UpdateDisplayName(ctx, first, "Morwen of the Hills"))
	require.NoError(t, repo.UpdateDisplayName(ctx, second, "Mor-wen"), "the key is free once renamed")

	// 64 characters whose key folds to 128: æ to "ae" and ß to "ss".
	long := strings.Repeat("ßæ", 32)
	require.NoError(t, repo.UpdateDisplayName(ctx, first, long))
	assert.Equal(t, long, getUser(t, repo, first).DisplayName)
	assert.Equal(t, new(strings.Repeat("ssae", 32)), displayNameKeys(t, repo)[first])

	// Names without a letter or digit have no key, so they never collide.
	require.NoError(t, repo.UpdateDisplayName(ctx, first, "…"))
	require.NoError(t, repo.UpdateDisplayName(ctx, second, "…"))
	keys := displayNameKeys(t, repo)
	assert.Nil(t, keys[first])
	assert.Nil(t, keys[second])

	require.NoError(t, repo.UpdateDisplayName(ctx, first, "Wren"))
	created := ensureUser(t, repo, third, "W.ren")
	assert.Equal(t, "W.ren", created.DisplayName, "a collision does not refuse a new user")
	assert.Nil(t, displayNameKeys(t, repo)[third])
	assert.Equal(t, new("wren"), displayNameKeys(t, repo)[first])

	require.ErrorIs(t, repo.SetDisplayNameKey(ctx, third, new("wren")), datasources.ErrDisplayNameTaken)
	require.NoError(t, repo.SetDisplayNameKey(ctx, second, nil))
	require.ErrorIs(t, repo.SetDisplayNameKey(ctx, "auth0|missing", nil), datasources.ErrNotFound)
	require.ErrorIs(t, repo.UpdateDisplayName(ctx, "auth0|missing", "Ash"), datasources.ErrNotFound)
}
