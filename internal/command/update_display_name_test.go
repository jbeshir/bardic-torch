package command

import (
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateDisplayName_StoresNormalizedNameAndReturnsUser(t *testing.T) {
	t.Parallel()

	repo := &fakerepo.Repo{Users: map[string]domain.User{
		resolvedUserID: {ID: resolvedUserID, DisplayName: testDisplayName, Role: domain.RoleModerator, CreatedAt: fixedNow},
	}}

	// Padded, and with "é" decomposed into "e" and a combining accent.
	user, err := (&UpdateDisplayName{Users: repo}).Execute(t.Context(),
		UpdateDisplayNameRequest{UserID: resolvedUserID, DisplayName: "  Renée "})

	require.NoError(t, err)
	assert.Equal(t, "Ren\u00e9e", user.DisplayName)
	assert.Equal(t, user, repo.Users[resolvedUserID])
}

func TestUpdateDisplayName_InvalidNameIsNotStored(t *testing.T) {
	t.Parallel()

	repo := &fakerepo.Repo{Users: map[string]domain.User{resolvedUserID: {ID: resolvedUserID, DisplayName: testDisplayName}}}
	_, err := (&UpdateDisplayName{Users: repo}).Execute(t.Context(),
		UpdateDisplayNameRequest{UserID: resolvedUserID, DisplayName: "  "})

	require.ErrorIs(t, err, domain.ErrInvalidDisplayName)
	assert.Equal(t, testDisplayName, repo.Users[resolvedUserID].DisplayName)
}
