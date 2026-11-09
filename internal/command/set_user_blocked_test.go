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

// setUserBlockedMocks are the datasources of a SetUserBlocked command; each fails the test if it is called
// unexpectedly.
type setUserBlockedMocks struct {
	blocker   *mocks.UserBlocker
	unblocker *mocks.UserUnblocker
	getter    *mocks.UserGetter
	publicIDs *mocks.PublicIDSetter
	cmd       *SetUserBlocked
}

func newSetUserBlockedMocks(t *testing.T) setUserBlockedMocks {
	m := setUserBlockedMocks{
		blocker:   mocks.NewUserBlocker(t),
		unblocker: mocks.NewUserUnblocker(t),
		getter:    mocks.NewUserGetter(t),
		publicIDs: mocks.NewPublicIDSetter(t),
	}
	m.cmd = &SetUserBlocked{Blocker: m.blocker, Unblocker: m.unblocker, Getter: m.getter, PublicIDSetter: m.publicIDs,
		PublicIDs: publicIDSequence(), Now: fixedClock}
	return m
}

// blockRequest is an interactive moderator's request to block or unblock resolvedUserID.
func blockRequest(blocked bool) SetUserBlockedRequest {
	return SetUserBlockedRequest{Principal: interactiveMod, UserID: resolvedUserID, Blocked: blocked}
}

// expectSet expects one block or unblock of resolvedUserID, returning err.
func (m setUserBlockedMocks) expectSet(blocked bool, err error) {
	if blocked {
		m.blocker.EXPECT().BlockUser(mock.Anything, moderatorUser.ID, resolvedUserID, fixedNow).Return(err).Once()
	} else {
		m.unblocker.EXPECT().UnblockUser(mock.Anything, moderatorUser.ID, resolvedUserID).Return(err).Once()
	}
}

func TestSetUserBlocked_ReturnsUpdatedUser(t *testing.T) {
	t.Parallel()

	blockedAt := fixedNow.Add(-time.Hour)
	for _, blocked := range []bool{true, false} {
		t.Run(blockedName(blocked), func(t *testing.T) {
			t.Parallel()

			stored := domain.User{ID: resolvedUserID, PublicID: testPublicID, DisplayName: testDisplayName,
				Role: domain.RoleContributor, CreatedAt: fixedNow}
			if blocked {
				// Blocking an already blocked user keeps the original time, which the stored user reflects.
				stored.BlockedAt = &blockedAt
			}
			m := newSetUserBlockedMocks(t)
			m.expectSet(blocked, nil)
			m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(stored, nil)

			user, err := m.cmd.Execute(t.Context(), blockRequest(blocked))

			require.NoError(t, err)
			assert.Equal(t, stored, user)
		})
	}
}

func TestSetUserBlocked_UnknownUserIsNotFound(t *testing.T) {
	t.Parallel()

	for _, blocked := range []bool{true, false} {
		t.Run(blockedName(blocked), func(t *testing.T) {
			t.Parallel()

			m := newSetUserBlockedMocks(t)
			m.expectSet(blocked, datasources.ErrNotFound)
			m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, datasources.ErrNotFound)

			_, err := m.cmd.Execute(t.Context(), blockRequest(blocked))

			require.ErrorIs(t, err, datasources.ErrNotFound)
			require.NotErrorIs(t, err, ErrModeratorNotBlockable)
		})
	}
}

func TestSetUserBlocked_ModeratorIsNotBlockable(t *testing.T) {
	t.Parallel()

	moderator := domain.User{ID: resolvedUserID, DisplayName: testDisplayName, Role: domain.RoleModerator, CreatedAt: fixedNow}
	for _, blocked := range []bool{true, false} {
		t.Run(blockedName(blocked), func(t *testing.T) {
			t.Parallel()

			m := newSetUserBlockedMocks(t)
			m.expectSet(blocked, datasources.ErrNotFound)
			m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(moderator, nil)

			_, err := m.cmd.Execute(t.Context(), blockRequest(blocked))

			require.ErrorIs(t, err, ErrModeratorNotBlockable)
		})
	}
}

func TestSetUserBlocked_RetriesContributorCreatedDuringUpdate(t *testing.T) {
	t.Parallel()

	created := domain.User{ID: resolvedUserID, PublicID: testPublicID, DisplayName: testDisplayName,
		Role: domain.RoleContributor, CreatedAt: fixedNow}
	for _, blocked := range []bool{true, false} {
		t.Run(blockedName(blocked), func(t *testing.T) {
			t.Parallel()

			updated := created
			if blocked {
				updated.BlockedAt = new(fixedNow)
			}
			// The user's first request creates them between the update, which finds nobody, and the read.
			m := newSetUserBlockedMocks(t)
			m.expectSet(blocked, datasources.ErrNotFound)
			m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(created, nil).Once()
			m.expectSet(blocked, nil)
			m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(updated, nil).Once()

			user, err := m.cmd.Execute(t.Context(), blockRequest(blocked))

			require.NoError(t, err)
			assert.Equal(t, updated, user)
		})
	}
}

func TestSetUserBlocked_ContributorNeverMatchingFails(t *testing.T) {
	t.Parallel()

	contributor := domain.User{ID: resolvedUserID, PublicID: testPublicID, DisplayName: testDisplayName,
		Role: domain.RoleContributor, CreatedAt: fixedNow}
	m := newSetUserBlockedMocks(t)
	m.expectSet(true, datasources.ErrNotFound)
	m.expectSet(true, datasources.ErrNotFound)
	m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(contributor, nil).Twice()

	_, err := m.cmd.Execute(t.Context(), blockRequest(true))

	require.ErrorIs(t, err, errContributorNotMatched)
	require.NotErrorIs(t, err, ErrModeratorNotBlockable)
}

func TestSetUserBlocked_DatasourceErrors(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")

	for _, blocked := range []bool{true, false} {
		t.Run(blockedName(blocked)+"_set", func(t *testing.T) {
			t.Parallel()

			m := newSetUserBlockedMocks(t)
			m.expectSet(blocked, dbErr)

			_, err := m.cmd.Execute(t.Context(), blockRequest(blocked))

			require.ErrorIs(t, err, dbErr)
		})

		t.Run(blockedName(blocked)+"_get", func(t *testing.T) {
			t.Parallel()

			m := newSetUserBlockedMocks(t)
			m.expectSet(blocked, nil)
			m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, dbErr)

			_, err := m.cmd.Execute(t.Context(), blockRequest(blocked))

			require.ErrorIs(t, err, dbErr)
		})
	}
}

func blockedName(blocked bool) string {
	if blocked {
		return "block"
	}
	return "unblock"
}

// TestSetUserBlocked_ModeratorRequired checks that only an interactive moderator may block or unblock: a
// contributor, and a moderator's API token, which has contributor powers only, are refused before any datasource
// is called.
func TestSetUserBlocked_ModeratorRequired(t *testing.T) {
	t.Parallel()

	principals := map[string]domain.Principal{"auth0_contributor": contributor, "moderator_token": moderatorToken}
	for name, principal := range principals {
		for _, blocked := range []bool{true, false} {
			t.Run(name+"_"+blockedName(blocked), func(t *testing.T) {
				t.Parallel()

				m := newSetUserBlockedMocks(t)
				_, err := m.cmd.Execute(t.Context(), SetUserBlockedRequest{Principal: principal, UserID: resolvedUserID,
					Blocked: blocked})

				require.ErrorIs(t, err, domain.ErrModeratorRequired)
			})
		}
	}
}

// TestSetUserBlocked_AssignsMissingPublicID checks that a user without a public ID, created before public IDs and
// with no request since, is given one before they are returned, as ResolveUser gives one (a taken ID is replaced
// by a fresh one), and is read again with it.
func TestSetUserBlocked_AssignsMissingPublicID(t *testing.T) {
	t.Parallel()

	for _, blocked := range []bool{true, false} {
		t.Run(blockedName(blocked), func(t *testing.T) {
			t.Parallel()

			stored := domain.User{ID: resolvedUserID, DisplayName: testDisplayName, Role: domain.RoleContributor,
				CreatedAt: fixedNow}
			if blocked {
				stored.BlockedAt = new(fixedNow)
			}
			withID := stored
			withID.PublicID = secondPublicID
			m := newSetUserBlockedMocks(t)
			m.expectSet(blocked, nil)
			m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(stored, nil).Once()
			m.publicIDs.EXPECT().SetUserPublicIDIfMissing(mock.Anything, resolvedUserID, firstPublicID).
				Return(datasources.ErrPublicIDTaken).Once()
			m.publicIDs.EXPECT().SetUserPublicIDIfMissing(mock.Anything, resolvedUserID, secondPublicID).
				Return(nil).Once()
			m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(withID, nil).Once()

			user, err := m.cmd.Execute(t.Context(), blockRequest(blocked))

			require.NoError(t, err)
			require.Truef(t, domain.IsPublicID(user.PublicID), "public_id %q", user.PublicID)
			assert.Equal(t, withID, user)
		})
	}
}
