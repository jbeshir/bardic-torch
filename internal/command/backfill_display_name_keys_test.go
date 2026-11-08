package command

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
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

// TestBackfillDisplayNameKeys_KeyTooLong checks that a stored name whose key would not fit its column, as a
// rollback to code that did not refuse such names may leave, does not fail startup: it is left without a key, a
// stale key it has is cleared, and each is logged once by user ID only.
func TestBackfillDisplayNameKeys_KeyTooLong(t *testing.T) {
	t.Parallel()

	oversized := strings.Repeat("ﷺ", 18)
	stale := "rowan"
	lister := mocks.NewDisplayNameKeyLister(t)
	lister.EXPECT().ListDisplayNameKeys(mock.Anything).Return([]datasources.UserDisplayNameKey{
		{UserID: "auth0|oversized", DisplayName: oversized},
		{UserID: "auth0|renamed", DisplayName: oversized, Key: &stale},
	}, nil)
	setter := mocks.NewDisplayNameKeySetter(t)
	setter.EXPECT().SetDisplayNameKey(mock.Anything, "auth0|renamed", (*string)(nil)).Return(nil).Once()

	var logs bytes.Buffer
	ctx := domain.ContextWithLogger(t.Context(), slog.New(slog.NewTextHandler(&logs, nil)))
	_, err := (&BackfillDisplayNameKeys{Lister: lister, Setter: setter}).Execute(ctx, Empty{})
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], "level=WARN")
	assert.Contains(t, lines[0], "user_id=auth0|oversized")
	assert.Contains(t, lines[1], "user_id=auth0|renamed")
	assert.NotContains(t, logs.String(), "ﷺ", "the log names the user, never the name")
}
