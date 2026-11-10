package command

import (
	"errors"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bootstrapUserID and otherBootstrapUserID are IDs to make moderators.
const (
	bootstrapUserID      = "google-oauth2|john"
	otherBootstrapUserID = "facebook|fabian"
)

func TestBootstrapModerators_PromotesEachUserOnce(t *testing.T) {
	t.Parallel()

	blockedAt := fixedNow
	repo := &fakerepo.Repo{Users: map[string]domain.User{
		bootstrapUserID: {ID: bootstrapUserID, DisplayName: testDisplayName, Role: domain.RoleContributor, BlockedAt: &blockedAt},
	}}

	_, err := (&BootstrapModerators{Users: repo, Now: fixedClock}).Execute(t.Context(),
		[]string{bootstrapUserID, otherBootstrapUserID, bootstrapUserID})

	require.NoError(t, err)
	// An existing user keeps their display name and is unblocked; a missing one is created with the default.
	assert.Equal(t, domain.User{ID: bootstrapUserID, DisplayName: testDisplayName, Role: domain.RoleModerator},
		repo.Users[bootstrapUserID])
	assert.Equal(t, domain.User{ID: otherBootstrapUserID, DisplayName: domain.DefaultDisplayName(otherBootstrapUserID),
		Role: domain.RoleModerator, CreatedAt: fixedNow}, repo.Users[otherBootstrapUserID])
}

func TestBootstrapModerators_IDsAreCaseSensitive(t *testing.T) {
	t.Parallel()

	repo := &fakerepo.Repo{}
	_, err := (&BootstrapModerators{Users: repo, Now: fixedClock}).Execute(t.Context(), []string{"auth0|b", "auth0|B"})

	require.NoError(t, err)
	assert.Len(t, repo.Users, 2)
}

func TestBootstrapModerators_NoIDsPromotesNobody(t *testing.T) {
	t.Parallel()

	repo := &fakerepo.Repo{}
	_, err := (&BootstrapModerators{Users: repo, Now: fixedClock}).Execute(t.Context(), nil)

	require.NoError(t, err)
	assert.Empty(t, repo.Users)
}

func TestBootstrapModerators_StopsAtFirstError(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")
	repo := &fakerepo.Repo{Err: dbErr}
	_, err := (&BootstrapModerators{Users: repo, Now: fixedClock}).Execute(t.Context(),
		[]string{bootstrapUserID, otherBootstrapUserID})

	require.ErrorIs(t, err, dbErr)
	assert.Empty(t, repo.Users)
}
