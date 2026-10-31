package command

import (
	"errors"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const updatedName = "Morwen"

func TestUpdateDisplayName_StoresNormalizedNameAndReturnsUser(t *testing.T) {
	t.Parallel()

	updated := domain.User{ID: resolvedUserID, DisplayName: "Ren\u00e9e", Role: domain.RoleModerator, CreatedAt: fixedNow}
	updater := mocks.NewDisplayNameUpdater(t)
	updater.EXPECT().UpdateDisplayName(mock.Anything, resolvedUserID, "Ren\u00e9e").Return(nil)
	getter := mocks.NewUserGetter(t)
	getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(updated, nil)

	cmd := &UpdateDisplayName{Updater: updater, Getter: getter}
	// Padded, and with "é" decomposed into "e" and a combining accent.
	user, err := cmd.Execute(t.Context(), UpdateDisplayNameRequest{UserID: resolvedUserID, DisplayName: "  Rene\u0301e "})

	require.NoError(t, err)
	assert.Equal(t, updated, user)
}

func TestUpdateDisplayName_InvalidNameIsNotStored(t *testing.T) {
	t.Parallel()

	// The mocks fail the test if they are called.
	cmd := &UpdateDisplayName{Updater: mocks.NewDisplayNameUpdater(t), Getter: mocks.NewUserGetter(t)}
	_, err := cmd.Execute(t.Context(), UpdateDisplayNameRequest{UserID: resolvedUserID, DisplayName: "  "})

	require.ErrorIs(t, err, domain.ErrInvalidDisplayName)
}

func TestUpdateDisplayName_DatasourceErrors(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")

	t.Run("update", func(t *testing.T) {
		t.Parallel()

		updater := mocks.NewDisplayNameUpdater(t)
		updater.EXPECT().UpdateDisplayName(mock.Anything, resolvedUserID, updatedName).Return(dbErr)

		cmd := &UpdateDisplayName{Updater: updater, Getter: mocks.NewUserGetter(t)}
		_, err := cmd.Execute(t.Context(), UpdateDisplayNameRequest{UserID: resolvedUserID, DisplayName: updatedName})

		require.ErrorIs(t, err, dbErr)
		require.NotErrorIs(t, err, domain.ErrInvalidDisplayName)
	})

	t.Run("get", func(t *testing.T) {
		t.Parallel()

		updater := mocks.NewDisplayNameUpdater(t)
		updater.EXPECT().UpdateDisplayName(mock.Anything, resolvedUserID, updatedName).Return(nil)
		getter := mocks.NewUserGetter(t)
		getter.EXPECT().GetUser(mock.Anything, resolvedUserID).Return(domain.User{}, dbErr)

		cmd := &UpdateDisplayName{Updater: updater, Getter: getter}
		_, err := cmd.Execute(t.Context(), UpdateDisplayNameRequest{UserID: resolvedUserID, DisplayName: updatedName})

		require.ErrorIs(t, err, dbErr)
	})
}
