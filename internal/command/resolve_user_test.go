package command

import (
	"errors"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const resolvedUserID = "google-oauth2|123"

var fixedNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func fixedClock() time.Time {
	return fixedNow
}

func TestResolveUser_ExistingUserIsReturnedWithoutWriting(t *testing.T) {
	t.Parallel()

	blockedAt := fixedNow.Add(-time.Hour)
	existing := domain.User{
		ID:          resolvedUserID,
		DisplayName: "Morwen",
		Role:        domain.RoleModerator,
		CreatedAt:   fixedNow.Add(-24 * time.Hour),
		BlockedAt:   &blockedAt,
	}
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(existing, nil)

	// The ensurer mock fails the test if it is called.
	cmd := &ResolveUser{Getter: getter, Ensurer: mocks.NewUserEnsurer(t), Now: fixedClock}
	user, err := cmd.Execute(t.Context(), resolvedUserID)

	require.NoError(t, err)
	assert.Equal(t, existing, user)
}

func TestResolveUser_CreatesMissingUser(t *testing.T) {
	t.Parallel()

	created := domain.User{
		ID:          resolvedUserID,
		DisplayName: domain.DefaultDisplayName(resolvedUserID),
		Role:        domain.RoleContributor,
		CreatedAt:   fixedNow,
	}
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, datasources.ErrNotFound)
	ensurer := mocks.NewUserEnsurer(t)
	ensurer.EXPECT().EnsureUser(mock.Anything, datasources.NewUser{
		ID:          resolvedUserID,
		DisplayName: domain.DefaultDisplayName(resolvedUserID),
		CreatedAt:   fixedNow,
	}).Return(created, nil)

	cmd := &ResolveUser{Getter: getter, Ensurer: ensurer, Now: fixedClock}
	user, err := cmd.Execute(t.Context(), resolvedUserID)

	require.NoError(t, err)
	assert.Equal(t, created, user)
}

func TestResolveUser_ReturnsUserStoredByConcurrentRequest(t *testing.T) {
	t.Parallel()

	// Another request created the user between the read and the insert: EnsureUser returns the stored user,
	// whatever it was created with.
	stored := domain.User{ID: resolvedUserID, DisplayName: "Morwen", Role: domain.RoleModerator, CreatedAt: fixedNow}
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, datasources.ErrNotFound)
	ensurer := mocks.NewUserEnsurer(t)
	ensurer.EXPECT().EnsureUser(mock.Anything, mock.Anything).Return(stored, nil)

	cmd := &ResolveUser{Getter: getter, Ensurer: ensurer, Now: fixedClock}
	user, err := cmd.Execute(t.Context(), resolvedUserID)

	require.NoError(t, err)
	assert.Equal(t, stored, user)
}

func TestResolveUser_GetErrorIsReturnedWithoutCreating(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, dbErr)

	cmd := &ResolveUser{Getter: getter, Ensurer: mocks.NewUserEnsurer(t), Now: fixedClock}
	_, err := cmd.Execute(t.Context(), resolvedUserID)

	require.ErrorIs(t, err, dbErr)
}

func TestResolveUser_EnsureErrorIsReturned(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, datasources.ErrNotFound)
	ensurer := mocks.NewUserEnsurer(t)
	ensurer.EXPECT().EnsureUser(mock.Anything, mock.Anything).Return(domain.User{}, dbErr)

	cmd := &ResolveUser{Getter: getter, Ensurer: ensurer, Now: fixedClock}
	_, err := cmd.Execute(t.Context(), resolvedUserID)

	require.ErrorIs(t, err, dbErr)
}
