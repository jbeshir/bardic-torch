//go:build integration

package mysql_test

import (
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// baseTime is whole seconds because DATETIME columns store no fractional seconds.
var baseTime = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

const moderatorID = "auth0|moderator"

func newRepository(t *testing.T) *mysql.Repository {
	t.Helper()

	return mysql.New(mysqltest.NewDatabase(t))
}

func ensureUser(t *testing.T, repo *mysql.Repository, userID, displayName string) domain.User {
	t.Helper()

	user, err := repo.EnsureUser(t.Context(), datasources.NewUser{
		ID:          userID,
		DisplayName: displayName,
		CreatedAt:   baseTime,
	})
	require.NoError(t, err)

	return user
}

func getUser(t *testing.T, repo *mysql.Repository, userID string) domain.User {
	t.Helper()

	user, err := repo.GetUser(t.Context(), userID)
	require.NoError(t, err)

	return user
}

func TestRepository_Ping(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	repo := mysql.New(db)
	require.NoError(t, repo.Ping(t.Context()))

	require.NoError(t, db.Close())
	require.ErrorContains(t, repo.Ping(t.Context()), "pinging MySQL DB")
}

func TestRepository_GetUser_NotFound(t *testing.T) {
	t.Parallel()

	_, err := newRepository(t).GetUser(t.Context(), "auth0|missing")
	require.ErrorIs(t, err, datasources.ErrNotFound)
}

func TestRepository_EnsureUser_CreatesContributor(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	want := domain.User{ID: "google-oauth2|1001", DisplayName: "Bard 1a2b3c4d", Role: domain.RoleContributor,
		CreatedAt: baseTime}

	assert.Equal(t, want, ensureUser(t, repo, want.ID, want.DisplayName))
	assert.Equal(t, want, getUser(t, repo, want.ID))
}

func TestRepository_EnsureUser_PreservesExistingUser(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()

	ensureUser(t, repo, moderatorID, "Original")
	require.NoError(t, repo.PromoteToModerator(ctx, datasources.NewUser{ID: moderatorID, DisplayName: "Other",
		CreatedAt: baseTime}))
	ensureUser(t, repo, "auth0|blocked", "Original")
	require.NoError(t, repo.BlockUser(ctx, "auth0|blocked", baseTime.Add(time.Hour)))
	ensureUser(t, repo, "auth0|renamed", "Original")
	require.NoError(t, repo.UpdateDisplayName(ctx, "auth0|renamed", "Renamed"))

	for _, userID := range []string{moderatorID, "auth0|blocked", "auth0|renamed"} {
		before := getUser(t, repo, userID)

		after, err := repo.EnsureUser(ctx, datasources.NewUser{
			ID:          userID,
			DisplayName: "Replacement",
			CreatedAt:   baseTime.Add(24 * time.Hour),
		})
		require.NoError(t, err)
		assert.Equal(t, before, after, userID)
		assert.Equal(t, before, getUser(t, repo, userID), userID)
	}

	assert.Equal(t, domain.RoleModerator, getUser(t, repo, moderatorID).Role)
	assert.True(t, getUser(t, repo, "auth0|blocked").IsBlocked())
	assert.Equal(t, "Renamed", getUser(t, repo, "auth0|renamed").DisplayName)
}

func TestRepository_EnsureUser_ConcurrentFirstCallsCreateOneUser(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	repo := mysql.New(db)
	const userID = "auth0|concurrent"
	const calls = 10

	users := make([]domain.User, calls)
	errs := make([]error, calls)
	var wg sync.WaitGroup
	for i := range calls {
		wg.Go(func() {
			users[i], errs[i] = repo.EnsureUser(t.Context(), datasources.NewUser{
				ID:          userID,
				DisplayName: "Bard concurrent",
				CreatedAt:   baseTime,
			})
		})
	}
	wg.Wait()

	for i := range calls {
		require.NoError(t, errs[i])
		assert.Equal(t, users[0], users[i])
	}

	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM users WHERE id = ?", userID).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestRepository_EnsureUser_IDsAreCaseSensitive(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)

	lower := ensureUser(t, repo, "google-oauth2|abc", "Lower")
	upper := ensureUser(t, repo, "google-oauth2|ABC", "Upper")

	assert.Equal(t, "Lower", lower.DisplayName)
	assert.Equal(t, "Upper", upper.DisplayName)
	assert.Equal(t, "Lower", getUser(t, repo, "google-oauth2|abc").DisplayName)
	assert.Equal(t, "Upper", getUser(t, repo, "google-oauth2|ABC").DisplayName)
}

func TestRepository_PromoteToModerator_CreatesMissingUser(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	require.NoError(t, repo.PromoteToModerator(t.Context(), datasources.NewUser{
		ID:          "auth0|new-moderator",
		DisplayName: "Bard 0badf00d",
		CreatedAt:   baseTime,
	}))

	assert.Equal(t, domain.User{ID: "auth0|new-moderator", DisplayName: "Bard 0badf00d", Role: domain.RoleModerator,
		CreatedAt: baseTime}, getUser(t, repo, "auth0|new-moderator"))
}

func TestRepository_PromoteToModerator_KeepsExistingUserAndIsIdempotent(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()

	ensureUser(t, repo, "auth0|promoted", "Chosen name")

	for range 2 {
		require.NoError(t, repo.PromoteToModerator(ctx, datasources.NewUser{
			ID:          "auth0|promoted",
			DisplayName: "Default name",
			CreatedAt:   baseTime.Add(24 * time.Hour),
		}))
	}

	assert.Equal(t, domain.User{ID: "auth0|promoted", DisplayName: "Chosen name", Role: domain.RoleModerator,
		CreatedAt: baseTime}, getUser(t, repo, "auth0|promoted"))
}

func TestRepository_PromoteToModerator_UnblocksBlockedContributor(t *testing.T) {
	t.Parallel()

	const bootstrappedID = "auth0|bootstrapped"

	repo := newRepository(t)
	ctx := t.Context()

	ensureUser(t, repo, bootstrappedID, "Chosen name")
	require.NoError(t, repo.BlockUser(ctx, bootstrappedID, baseTime.Add(time.Hour)))

	require.NoError(t, repo.PromoteToModerator(ctx, datasources.NewUser{
		ID:          bootstrappedID,
		DisplayName: "Default name",
		CreatedAt:   baseTime.Add(24 * time.Hour),
	}))

	assert.Equal(t, domain.User{ID: bootstrappedID, DisplayName: "Chosen name", Role: domain.RoleModerator,
		CreatedAt: baseTime}, getUser(t, repo, bootstrappedID))
}

func TestRepository_UpdateDisplayName(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	ensureUser(t, repo, "auth0|named", "Before")

	require.NoError(t, repo.UpdateDisplayName(ctx, "auth0|named", "Ünïcödé name"))
	assert.Equal(t, "Ünïcödé name", getUser(t, repo, "auth0|named").DisplayName)

	// Setting the current name changes no rows but still matches one, so it is not reported as missing.
	require.NoError(t, repo.UpdateDisplayName(ctx, "auth0|named", "Ünïcödé name"))

	require.ErrorIs(t, repo.UpdateDisplayName(ctx, "auth0|missing", "Name"), datasources.ErrNotFound)
	require.ErrorIs(t, repo.UpdateDisplayName(ctx, "AUTH0|NAMED", "Name"), datasources.ErrNotFound)
}

func TestRepository_BlockUnblock_ReblockKeepsFirstBlockedAt(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	firstBlock := baseTime.Add(time.Hour)
	ensureUser(t, repo, "auth0|contributor", "Contributor")

	require.NoError(t, repo.BlockUser(ctx, "auth0|contributor", firstBlock))
	require.NoError(t, repo.BlockUser(ctx, "auth0|contributor", firstBlock.Add(time.Hour)))
	assert.Equal(t, &firstBlock, getUser(t, repo, "auth0|contributor").BlockedAt)

	require.NoError(t, repo.UnblockUser(ctx, "auth0|contributor"))
	assert.False(t, getUser(t, repo, "auth0|contributor").IsBlocked())
	require.NoError(t, repo.UnblockUser(ctx, "auth0|contributor"))

	secondBlock := firstBlock.Add(2 * time.Hour)
	require.NoError(t, repo.BlockUser(ctx, "auth0|contributor", secondBlock))
	assert.Equal(t, &secondBlock, getUser(t, repo, "auth0|contributor").BlockedAt)
}

func TestRepository_BlockUnblock_ModeratorTargetMatchesNoRows(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	require.NoError(t, repo.PromoteToModerator(ctx, datasources.NewUser{ID: moderatorID, DisplayName: "Moderator",
		CreatedAt: baseTime}))
	before := getUser(t, repo, moderatorID)

	require.ErrorIs(t, repo.BlockUser(ctx, moderatorID, baseTime.Add(time.Hour)), datasources.ErrNotFound)
	require.ErrorIs(t, repo.UnblockUser(ctx, moderatorID), datasources.ErrNotFound)
	assert.Equal(t, before, getUser(t, repo, moderatorID))
}

func TestRepository_BlockUnblock_UnknownUserMatchesNoRows(t *testing.T) {
	t.Parallel()

	repo := newRepository(t)
	ctx := t.Context()
	ensureUser(t, repo, "auth0|known", "Known")

	require.ErrorIs(t, repo.BlockUser(ctx, "auth0|unknown", baseTime), datasources.ErrNotFound)
	require.ErrorIs(t, repo.UnblockUser(ctx, "auth0|unknown"), datasources.ErrNotFound)
	require.ErrorIs(t, repo.BlockUser(ctx, "AUTH0|KNOWN", baseTime), datasources.ErrNotFound)
	assert.False(t, getUser(t, repo, "auth0|known").IsBlocked())
}
