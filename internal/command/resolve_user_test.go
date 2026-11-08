package command

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	resolvedUserID  = "google-oauth2|123"
	testDisplayName = "Morwen"
	testPublicID    = "abcdefghijklmnop"
	// firstPublicID, secondPublicID and thirdPublicID are what publicIDSequence issues, in turn.
	firstPublicID  = "aaaaaaaaaaaaaaa2"
	secondPublicID = "aaaaaaaaaaaaaaa3"
	thirdPublicID  = "aaaaaaaaaaaaaaa4"
)

var fixedNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func fixedClock() time.Time {
	return fixedNow
}

// publicIDSequence is a PublicIDSource issuing firstPublicID, secondPublicID and so on, in turn.
func publicIDSequence() domain.PublicIDSource {
	issued := 0
	return func() string {
		issued++
		return fmt.Sprintf("aaaaaaaaaaaaaaa%d", issued+1)
	}
}

// unusedPublicIDs is a PublicIDSource that fails the test if it is called.
func unusedPublicIDs(t *testing.T) domain.PublicIDSource {
	return func() string {
		t.Error("public ID issued unexpectedly")
		return ""
	}
}

func TestResolveUser_ExistingUserIsReturnedWithoutWriting(t *testing.T) {
	t.Parallel()

	blockedAt := fixedNow.Add(-time.Hour)
	existing := domain.User{
		ID:          resolvedUserID,
		PublicID:    testPublicID,
		DisplayName: testDisplayName,
		Role:        domain.RoleModerator,
		CreatedAt:   fixedNow.Add(-24 * time.Hour),
		BlockedAt:   &blockedAt,
	}
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(existing, nil)

	// The ensurer mock fails the test if it is called.
	cmd := &ResolveUser{Getter: getter, Ensurer: mocks.NewUserEnsurer(t), PublicIDs: unusedPublicIDs(t), Now: fixedClock}
	user, err := cmd.Execute(t.Context(), resolvedUserID)

	require.NoError(t, err)
	assert.Equal(t, existing, user)
}

func TestResolveUser_CreatesMissingUser(t *testing.T) {
	t.Parallel()

	created := domain.User{
		ID:          resolvedUserID,
		PublicID:    firstPublicID,
		DisplayName: domain.DefaultDisplayName(resolvedUserID),
		Role:        domain.RoleContributor,
		CreatedAt:   fixedNow,
	}
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, datasources.ErrNotFound)
	ensurer := mocks.NewUserEnsurer(t)
	ensurer.EXPECT().EnsureUser(mock.Anything, datasources.NewUser{
		ID:          resolvedUserID,
		PublicID:    firstPublicID,
		DisplayName: domain.DefaultDisplayName(resolvedUserID),
		CreatedAt:   fixedNow,
	}).Return(created, nil)

	cmd := &ResolveUser{Getter: getter, Ensurer: ensurer, PublicIDs: publicIDSequence(), Now: fixedClock}
	user, err := cmd.Execute(t.Context(), resolvedUserID)

	require.NoError(t, err)
	assert.Equal(t, created, user)
}

// TestResolveUser_GivesExistingUserWithoutPublicIDOne covers a user created without a public ID since the startup
// backfill ran, by a release that does not set them: the ensurer, which leaves the rest of an existing user as
// stored, gives them one.
func TestResolveUser_GivesExistingUserWithoutPublicIDOne(t *testing.T) {
	t.Parallel()

	stored := domain.User{ID: resolvedUserID, DisplayName: testDisplayName, Role: domain.RoleModerator,
		CreatedAt: fixedNow.Add(-time.Hour)}
	given := stored
	given.PublicID = firstPublicID
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(stored, nil)
	ensurer := mocks.NewUserEnsurer(t)
	ensurer.EXPECT().EnsureUser(mock.Anything, mock.MatchedBy(func(u datasources.NewUser) bool {
		return u.ID == resolvedUserID && u.PublicID == firstPublicID
	})).Return(given, nil)

	cmd := &ResolveUser{Getter: getter, Ensurer: ensurer, PublicIDs: publicIDSequence(), Now: fixedClock}
	user, err := cmd.Execute(t.Context(), resolvedUserID)

	require.NoError(t, err)
	assert.Equal(t, given, user)
}

func TestResolveUser_PublicIDTakenIsRetriedWithAnother(t *testing.T) {
	t.Parallel()

	created := domain.User{ID: resolvedUserID, PublicID: secondPublicID, Role: domain.RoleContributor}
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, datasources.ErrNotFound)
	ensurer := mocks.NewUserEnsurer(t)
	ensurer.EXPECT().EnsureUser(mock.Anything, mock.MatchedBy(func(u datasources.NewUser) bool {
		return u.PublicID == firstPublicID
	})).Return(domain.User{}, fmt.Errorf("setting: %w", datasources.ErrPublicIDTaken)).Once()
	ensurer.EXPECT().EnsureUser(mock.Anything, mock.MatchedBy(func(u datasources.NewUser) bool {
		return u.PublicID == secondPublicID
	})).Return(created, nil).Once()

	cmd := &ResolveUser{Getter: getter, Ensurer: ensurer, PublicIDs: publicIDSequence(), Now: fixedClock}
	user, err := cmd.Execute(t.Context(), resolvedUserID)

	require.NoError(t, err)
	assert.Equal(t, created, user)
}

func TestResolveUser_PublicIDTakenEveryTimeFails(t *testing.T) {
	t.Parallel()

	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, datasources.ErrNotFound)
	ensurer := mocks.NewUserEnsurer(t)
	ensurer.EXPECT().EnsureUser(mock.Anything, mock.Anything).Return(domain.User{}, datasources.ErrPublicIDTaken).
		Times(maxPublicIDAttempts)

	cmd := &ResolveUser{Getter: getter, Ensurer: ensurer, PublicIDs: publicIDSequence(), Now: fixedClock}
	_, err := cmd.Execute(t.Context(), resolvedUserID)

	require.ErrorIs(t, err, datasources.ErrPublicIDTaken)
}

func TestResolveUser_ReturnsUserStoredByConcurrentRequest(t *testing.T) {
	t.Parallel()

	// Another request created the user between the read and the insert: EnsureUser returns the stored user,
	// whatever it was created with.
	stored := domain.User{ID: resolvedUserID, PublicID: testPublicID, DisplayName: testDisplayName,
		Role: domain.RoleModerator, CreatedAt: fixedNow}
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, datasources.ErrNotFound)
	ensurer := mocks.NewUserEnsurer(t)
	ensurer.EXPECT().EnsureUser(mock.Anything, mock.Anything).Return(stored, nil)

	cmd := &ResolveUser{Getter: getter, Ensurer: ensurer, PublicIDs: publicIDSequence(), Now: fixedClock}
	user, err := cmd.Execute(t.Context(), resolvedUserID)

	require.NoError(t, err)
	assert.Equal(t, stored, user)
}

func TestResolveUser_GetErrorIsReturnedWithoutCreating(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, dbErr)

	cmd := &ResolveUser{Getter: getter, Ensurer: mocks.NewUserEnsurer(t), PublicIDs: unusedPublicIDs(t), Now: fixedClock}
	_, err := cmd.Execute(t.Context(), resolvedUserID)

	require.ErrorIs(t, err, dbErr)
}

func TestResolveUser_EnsureErrorIsReturned(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, datasources.ErrNotFound)
	ensurer := mocks.NewUserEnsurer(t)
	ensurer.EXPECT().EnsureUser(mock.Anything, mock.Anything).Return(domain.User{}, dbErr).Once()

	cmd := &ResolveUser{Getter: getter, Ensurer: ensurer, PublicIDs: publicIDSequence(), Now: fixedClock}
	_, err := cmd.Execute(t.Context(), resolvedUserID)

	require.ErrorIs(t, err, dbErr)
}
