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
	cmd       *SetUserBlocked
}

func newSetUserBlockedMocks(t *testing.T) setUserBlockedMocks {
	m := setUserBlockedMocks{
		blocker:   mocks.NewUserBlocker(t),
		unblocker: mocks.NewUserUnblocker(t),
		getter:    mocks.NewUserGetter(t),
	}
	m.cmd = &SetUserBlocked{Blocker: m.blocker, Unblocker: m.unblocker, Getter: m.getter, Now: fixedClock}
	return m
}

// expectSet expects the block or unblock of resolvedUserID, returning err.
func (m setUserBlockedMocks) expectSet(blocked bool, err error) {
	if blocked {
		m.blocker.EXPECT().BlockUser(mock.Anything, resolvedUserID, fixedNow).Return(err)
	} else {
		m.unblocker.EXPECT().UnblockUser(mock.Anything, resolvedUserID).Return(err)
	}
}

func TestSetUserBlocked_ReturnsUpdatedUser(t *testing.T) {
	t.Parallel()

	blockedAt := fixedNow.Add(-time.Hour)
	for _, blocked := range []bool{true, false} {
		t.Run(blockedName(blocked), func(t *testing.T) {
			t.Parallel()

			stored := domain.User{ID: resolvedUserID, DisplayName: testDisplayName, Role: domain.RoleContributor, CreatedAt: fixedNow}
			if blocked {
				// Blocking an already blocked user keeps the original time, which the stored user reflects.
				stored.BlockedAt = &blockedAt
			}
			m := newSetUserBlockedMocks(t)
			m.expectSet(blocked, nil)
			m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(stored, nil)

			user, err := m.cmd.Execute(t.Context(), SetUserBlockedRequest{UserID: resolvedUserID, Blocked: blocked})

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

			_, err := m.cmd.Execute(t.Context(), SetUserBlockedRequest{UserID: resolvedUserID, Blocked: blocked})

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

			_, err := m.cmd.Execute(t.Context(), SetUserBlockedRequest{UserID: resolvedUserID, Blocked: blocked})

			require.ErrorIs(t, err, ErrModeratorNotBlockable)
		})
	}
}

func TestSetUserBlocked_DatasourceErrors(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")

	for _, blocked := range []bool{true, false} {
		t.Run(blockedName(blocked)+"_set", func(t *testing.T) {
			t.Parallel()

			m := newSetUserBlockedMocks(t)
			m.expectSet(blocked, dbErr)

			_, err := m.cmd.Execute(t.Context(), SetUserBlockedRequest{UserID: resolvedUserID, Blocked: blocked})

			require.ErrorIs(t, err, dbErr)
		})

		t.Run(blockedName(blocked)+"_get", func(t *testing.T) {
			t.Parallel()

			m := newSetUserBlockedMocks(t)
			m.expectSet(blocked, nil)
			m.getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, dbErr)

			_, err := m.cmd.Execute(t.Context(), SetUserBlockedRequest{UserID: resolvedUserID, Blocked: blocked})

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
