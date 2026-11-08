//go:build integration

package mysql_test

import (
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
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

// TestRepository_DisplayNameKey_BlankLetter checks that a name holding a blank letter, stored before such names
// were refused, has the key of the name it looks like, so that it cannot be taken beside that name.
func TestRepository_DisplayNameKey_BlankLetter(t *testing.T) {
	t.Parallel()
	repo := newRepository(t)
	const first, second = "auth0|first", "auth0|second"
	ensureUser(t, repo, first, "Morwen")
	ensureUser(t, repo, second, "Bard second")

	require.ErrorIs(t, repo.UpdateDisplayName(t.Context(), second, "Morwen\u3164"), datasources.ErrDisplayNameTaken)
	assert.Equal(t, "Bard second", getUser(t, repo, second).DisplayName)
}

// TestBackfillDisplayNameKeys_KeyTooLongForColumn checks that a stored name whose key would not fit the
// display_name_key column, as code that did not refuse such names may have left, does not fail the backfill, and
// is left without a key.
func TestBackfillDisplayNameKeys_KeyTooLongForColumn(t *testing.T) {
	t.Parallel()
	db := catalogtest.Writable(t)
	repo := mysql.New(db)
	const oversized, other = "auth0|oversized", "auth0|other"
	_, err := db.ExecContext(t.Context(), "INSERT INTO users (id, display_name, role, created_at) "+
		"VALUES (?, ?, 'contributor', ?)", oversized, strings.Repeat("ﷺ", 18), baseTime)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "INSERT INTO users (id, display_name, role, created_at) "+
		"VALUES (?, 'Wren', 'contributor', ?)", other, baseTime)
	require.NoError(t, err)

	backfill := &command.BackfillDisplayNameKeys{Lister: repo, Setter: repo}
	_, err = backfill.Execute(t.Context(), command.Empty{})
	require.NoError(t, err)

	keys := displayNameKeys(t, repo)
	assert.Nil(t, keys[oversized])
	assert.Equal(t, new("wren"), keys[other])
}

// TestMigrations_DisplayNameKeyWidth checks that the display_name_key column holds exactly the longest key
// NormalizeDisplayName accepts, so that the two cannot drift apart.
func TestMigrations_DisplayNameKeyWidth(t *testing.T) {
	t.Parallel()

	var width int
	require.NoError(t, catalogtest.Writable(t).QueryRowContext(t.Context(), "SELECT CHARACTER_MAXIMUM_LENGTH "+
		"FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = 'users' "+
		"AND column_name = 'display_name_key'").Scan(&width))
	assert.Equal(t, domain.MaxDisplayNameKeyRunes, width)
}
