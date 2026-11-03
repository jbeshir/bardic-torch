//go:build integration

package mysql_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAPIToken returns token values for userID with a fresh ID and secret hash, created at createdAt.
func newAPIToken(userID string, name *string, createdAt time.Time) datasources.NewAPIToken {
	id := uuid.NewString()
	hash := sha256.Sum256([]byte("secret-" + id))
	hexHash := hex.EncodeToString(hash[:])

	return datasources.NewAPIToken{
		ID:          id,
		UserID:      userID,
		TokenHash:   hexHash,
		TokenPrefix: hexHash[:8],
		Name:        name,
		CreatedAt:   createdAt,
	}
}

// MySQL error numbers for violated constraints.
const (
	// errDuplicateEntry is ER_DUP_ENTRY: a unique index already has the value.
	errDuplicateEntry = 1062
	// errNoReferencedRow is ER_NO_REFERENCED_ROW_2: a foreign key names a row that does not exist.
	errNoReferencedRow = 1452
)

// requireMySQLError requires err to be a MySQL error with the given number.
func requireMySQLError(t *testing.T, err error, number uint16) {
	t.Helper()

	var mysqlErr *mysqldriver.MySQLError
	require.ErrorAs(t, err, &mysqlErr)
	assert.Equal(t, number, mysqlErr.Number, mysqlErr.Message)
}

func createAPIToken(t *testing.T, repo *mysql.Repository, token datasources.NewAPIToken) {
	t.Helper()

	require.NoError(t, repo.CreateAPIToken(t.Context(), token))
}

func getAPIToken(t *testing.T, repo *mysql.Repository, tokenHash string) domain.APIToken {
	t.Helper()

	token, err := repo.GetAPITokenByHash(t.Context(), tokenHash)
	require.NoError(t, err)

	return token
}

func tokenIDs(tokens []domain.APIToken) []string {
	ids := make([]string, 0, len(tokens))
	for _, token := range tokens {
		ids = append(ids, token.ID)
	}
	return ids
}

func TestRepository_APITokens_CreateAndGetByHash(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ensureUser(t, repo, "auth0|owner", "Owner")
	name := "laptop script"
	named := newAPIToken("auth0|owner", &name, baseTime)
	unnamed := newAPIToken("auth0|owner", nil, baseTime.Add(time.Second))
	createAPIToken(t, repo, named)
	createAPIToken(t, repo, unnamed)

	assert.Equal(t, domain.APIToken{ID: named.ID, UserID: "auth0|owner", Prefix: named.TokenPrefix, Name: &name,
		CreatedAt: baseTime}, getAPIToken(t, repo, named.TokenHash))
	assert.Equal(t, domain.APIToken{ID: unnamed.ID, UserID: "auth0|owner", Prefix: unnamed.TokenPrefix,
		CreatedAt: baseTime.Add(time.Second)}, getAPIToken(t, repo, unnamed.TokenHash))

	_, err := repo.GetAPITokenByHash(t.Context(), newAPIToken("auth0|owner", nil, baseTime).TokenHash)
	require.ErrorIs(t, err, datasources.ErrNotFound)
}

func TestRepository_APITokens_GetByHashReturnsRevokedToken(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ensureUser(t, repo, "auth0|owner", "Owner")
	token := newAPIToken("auth0|owner", nil, baseTime)
	createAPIToken(t, repo, token)
	revokedAt := baseTime.Add(time.Hour)
	require.NoError(t, repo.RevokeAPIToken(t.Context(), "auth0|owner", token.ID, revokedAt))

	got := getAPIToken(t, repo, token.TokenHash)
	assert.Equal(t, &revokedAt, got.RevokedAt)
	assert.False(t, got.IsActive())
}

func TestRepository_APITokens_CreateRequiresExistingUser(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ensureUser(t, repo, "auth0|owner", "Owner")

	// An ID differing only by a trailing space is another, missing, user.
	for _, userID := range []string{"auth0|missing", "auth0|owner "} {
		err := repo.CreateAPIToken(t.Context(), newAPIToken(userID, nil, baseTime))
		requireMySQLError(t, err, errNoReferencedRow)
	}
}

func TestRepository_APITokens_HashIsUnique(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ensureUser(t, repo, "auth0|owner", "Owner")
	token := newAPIToken("auth0|owner", nil, baseTime)
	createAPIToken(t, repo, token)

	duplicate := newAPIToken("auth0|owner", nil, baseTime)
	duplicate.TokenHash = token.TokenHash
	requireMySQLError(t, repo.CreateAPIToken(t.Context(), duplicate), errDuplicateEntry)
}

func TestRepository_APITokens_ListExcludesRevokedNewestFirst(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	ensureUser(t, repo, "auth0|owner", "Owner")
	ensureUser(t, repo, "auth0|other", "Other")

	oldest := newAPIToken("auth0|owner", nil, baseTime)
	revoked := newAPIToken("auth0|owner", nil, baseTime.Add(time.Minute))
	newestA := newAPIToken("auth0|owner", nil, baseTime.Add(2*time.Minute))
	newestB := newAPIToken("auth0|owner", nil, baseTime.Add(2*time.Minute))
	for _, token := range []datasources.NewAPIToken{oldest, revoked, newestA, newestB,
		newAPIToken("auth0|other", nil, baseTime)} {
		createAPIToken(t, repo, token)
	}
	require.NoError(t, repo.RevokeAPIToken(ctx, "auth0|owner", revoked.ID, baseTime.Add(time.Hour)))

	// Tokens created in the same second are ordered by ID.
	newest := []string{newestA.ID, newestB.ID}
	if newest[0] > newest[1] {
		newest[0], newest[1] = newest[1], newest[0]
	}

	tokens, err := repo.ListUserActiveAPITokens(ctx, "auth0|owner")
	require.NoError(t, err)
	assert.Equal(t, []string{newest[0], newest[1], oldest.ID}, tokenIDs(tokens))
	for _, token := range tokens {
		assert.True(t, token.IsActive())
		assert.Equal(t, "auth0|owner", token.UserID)
	}

	none, err := repo.ListUserActiveAPITokens(ctx, "auth0|nobody")
	require.NoError(t, err)
	assert.Empty(t, none)
}

func TestRepository_APITokens_CountActive(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	ensureUser(t, repo, "auth0|owner", "Owner")
	ensureUser(t, repo, "auth0|other", "Other")

	count, err := repo.CountUserActiveAPITokens(ctx, "auth0|owner")
	require.NoError(t, err)
	assert.Zero(t, count)

	tokens := []datasources.NewAPIToken{
		newAPIToken("auth0|owner", nil, baseTime),
		newAPIToken("auth0|owner", nil, baseTime),
		newAPIToken("auth0|owner", nil, baseTime),
		newAPIToken("auth0|other", nil, baseTime),
	}
	for _, token := range tokens {
		createAPIToken(t, repo, token)
	}
	require.NoError(t, repo.RevokeAPIToken(ctx, "auth0|owner", tokens[0].ID, baseTime))

	count, err = repo.CountUserActiveAPITokens(ctx, "auth0|owner")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestRepository_APITokens_RevokeIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	ensureUser(t, repo, "auth0|owner", "Owner")
	token := newAPIToken("auth0|owner", nil, baseTime)
	createAPIToken(t, repo, token)
	firstRevoke := baseTime.Add(time.Hour)

	require.NoError(t, repo.RevokeAPIToken(ctx, "auth0|owner", token.ID, firstRevoke))
	require.NoError(t, repo.RevokeAPIToken(ctx, "auth0|owner", token.ID, firstRevoke.Add(time.Hour)))

	assert.Equal(t, &firstRevoke, getAPIToken(t, repo, token.TokenHash).RevokedAt)
}

func TestRepository_APITokens_RevokeForeignOrUnknownTokenNotFound(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	ensureUser(t, repo, "auth0|owner", "Owner")
	ensureUser(t, repo, "auth0|other", "Other")
	token := newAPIToken("auth0|owner", nil, baseTime)
	createAPIToken(t, repo, token)

	require.ErrorIs(t, repo.RevokeAPIToken(ctx, "auth0|other", token.ID, baseTime), datasources.ErrNotFound)
	require.ErrorIs(t, repo.RevokeAPIToken(ctx, "AUTH0|OWNER", token.ID, baseTime), datasources.ErrNotFound)
	require.ErrorIs(t, repo.RevokeAPIToken(ctx, "auth0|owner ", token.ID, baseTime), datasources.ErrNotFound)
	require.ErrorIs(t, repo.RevokeAPIToken(ctx, "auth0|owner", uuid.NewString(), baseTime), datasources.ErrNotFound)
	assert.True(t, getAPIToken(t, repo, token.TokenHash).IsActive())
}

func TestRepository_APITokens_DeletingUserCascades(t *testing.T) {
	t.Parallel()

	db := catalogtest.Writable(t)
	repo := mysql.New(db)
	ctx := t.Context()
	ensureUser(t, repo, "auth0|owner", "Owner")
	ensureUser(t, repo, "auth0|other", "Other")
	owned := newAPIToken("auth0|owner", nil, baseTime)
	kept := newAPIToken("auth0|other", nil, baseTime)
	createAPIToken(t, repo, owned)
	createAPIToken(t, repo, kept)

	_, err := db.ExecContext(ctx, "DELETE FROM users WHERE id = ?", "auth0|owner")
	require.NoError(t, err)

	_, err = repo.GetAPITokenByHash(ctx, owned.TokenHash)
	require.ErrorIs(t, err, datasources.ErrNotFound)
	assert.Equal(t, kept.ID, getAPIToken(t, repo, kept.TokenHash).ID)
}

func TestRepository_UpdateAPITokenLastUsed_Throttled(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	ensureUser(t, repo, "auth0|owner", "Owner")
	token := newAPIToken("auth0|owner", nil, baseTime)
	createAPIToken(t, repo, token)
	firstUse := baseTime.Add(time.Hour)

	steps := []struct {
		usedAt time.Time
		want   time.Time
	}{
		{usedAt: firstUse, want: firstUse},
		{usedAt: firstUse.Add(30 * time.Second), want: firstUse},
		{usedAt: firstUse.Add(time.Minute), want: firstUse},
		{usedAt: firstUse.Add(time.Minute + time.Second), want: firstUse.Add(time.Minute + time.Second)},
		{usedAt: firstUse.Add(90 * time.Second), want: firstUse.Add(time.Minute + time.Second)},
	}
	for _, step := range steps {
		require.NoError(t, repo.UpdateAPITokenLastUsed(ctx, token.ID, step.usedAt))
		assert.Equal(t, &step.want, getAPIToken(t, repo, token.TokenHash).LastUsedAt, "used at %s", step.usedAt)
	}

	require.NoError(t, repo.UpdateAPITokenLastUsed(ctx, uuid.NewString(), firstUse))
}
