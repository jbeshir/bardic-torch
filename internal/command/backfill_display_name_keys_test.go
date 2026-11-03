package command

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestBackfillDisplayNameKeys checks that stale keys are cleared before any key is set, so that a key a user no
// longer holds is free for its new holder, that missing keys are set oldest user first, that a collision leaves the
// later user without a key, and that current keys and names without letters or digits are left alone.
func TestBackfillDisplayNameKeys(t *testing.T) {
	t.Parallel()

	current, stale := "morwen", "wren"
	lister := mocks.NewDisplayNameKeyLister(t)
	lister.EXPECT().ListDisplayNameKeys(mock.Anything).Return([]datasources.UserDisplayNameKey{
		{UserID: "auth0|current", DisplayName: "Morwen", Key: &current},
		// Renamed by code that did not set keys: "Wren" is now Rowan's, whose key is missing.
		{UserID: "auth0|renamed", DisplayName: "Ash", Key: &stale},
		{UserID: "auth0|rowan", DisplayName: "Wren"},
		// A later user whose name has Rowan's key.
		{UserID: "auth0|later", DisplayName: "W.ren"},
		{UserID: "auth0|none", DisplayName: "!!"},
	}, nil)
	setter := mocks.NewDisplayNameKeySetter(t)
	cleared := setter.EXPECT().SetDisplayNameKey(mock.Anything, "auth0|renamed", (*string)(nil)).Return(nil).Once()
	setter.EXPECT().SetDisplayNameKey(mock.Anything, "auth0|renamed", new("ash")).Return(nil).Once().
		NotBefore(cleared)
	setter.EXPECT().SetDisplayNameKey(mock.Anything, "auth0|rowan", new("wren")).Return(nil).Once().
		NotBefore(cleared)
	setter.EXPECT().SetDisplayNameKey(mock.Anything, "auth0|later", new("wren")).
		Return(datasources.ErrDisplayNameTaken).Once()

	_, err := (&BackfillDisplayNameKeys{Lister: lister, Setter: setter}).Execute(t.Context(), Empty{})
	require.NoError(t, err)
}

func TestBackfillDisplayNameKeys_Errors(t *testing.T) {
	t.Parallel()

	lister := mocks.NewDisplayNameKeyLister(t)
	lister.EXPECT().ListDisplayNameKeys(mock.Anything).Return(nil, errDatasource).Once()
	_, err := (&BackfillDisplayNameKeys{Lister: lister, Setter: mocks.NewDisplayNameKeySetter(t)}).Execute(
		t.Context(), Empty{})
	require.ErrorIs(t, err, errDatasource)

	lister.EXPECT().ListDisplayNameKeys(mock.Anything).Return([]datasources.UserDisplayNameKey{
		{UserID: "auth0|rowan", DisplayName: "Wren"},
	}, nil).Once()
	setter := mocks.NewDisplayNameKeySetter(t)
	setter.EXPECT().SetDisplayNameKey(mock.Anything, "auth0|rowan", new("wren")).Return(errDatasource)
	_, err = (&BackfillDisplayNameKeys{Lister: lister, Setter: setter}).Execute(t.Context(), Empty{})
	require.ErrorIs(t, err, errDatasource)
}
