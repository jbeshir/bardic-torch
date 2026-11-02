package command

import (
	"errors"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// bootstrapUserID and otherBootstrapUserID are IDs to make moderators.
const (
	bootstrapUserID      = "google-oauth2|john"
	otherBootstrapUserID = "facebook|fabian"
)

func newModerator(userID string) datasources.NewUser {
	return datasources.NewUser{ID: userID, DisplayName: domain.DefaultDisplayName(userID), CreatedAt: fixedNow}
}

func TestBootstrapModerators_PromotesEachUserOnce(t *testing.T) {
	t.Parallel()

	promoter := mocks.NewModeratorPromoter(t)
	promoter.EXPECT().PromoteToModerator(mock.Anything, newModerator(bootstrapUserID)).Return(nil).Once()
	promoter.EXPECT().PromoteToModerator(mock.Anything, newModerator(otherBootstrapUserID)).Return(nil).Once()

	cmd := &BootstrapModerators{Promoter: promoter, Now: fixedClock}
	_, err := cmd.Execute(t.Context(), []string{bootstrapUserID, otherBootstrapUserID, bootstrapUserID})

	require.NoError(t, err)
}

func TestBootstrapModerators_IDsAreCaseSensitive(t *testing.T) {
	t.Parallel()

	promoter := mocks.NewModeratorPromoter(t)
	promoter.EXPECT().PromoteToModerator(mock.Anything, newModerator("auth0|b")).Return(nil).Once()
	promoter.EXPECT().PromoteToModerator(mock.Anything, newModerator("auth0|B")).Return(nil).Once()

	cmd := &BootstrapModerators{Promoter: promoter, Now: fixedClock}
	_, err := cmd.Execute(t.Context(), []string{"auth0|b", "auth0|B"})

	require.NoError(t, err)
}

func TestBootstrapModerators_NoIDsPromotesNobody(t *testing.T) {
	t.Parallel()

	// The promoter mock fails the test if it is called.
	cmd := &BootstrapModerators{Promoter: mocks.NewModeratorPromoter(t), Now: fixedClock}
	_, err := cmd.Execute(t.Context(), nil)

	require.NoError(t, err)
}

func TestBootstrapModerators_StopsAtFirstError(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")
	promoter := mocks.NewModeratorPromoter(t)
	promoter.EXPECT().PromoteToModerator(mock.Anything, newModerator(bootstrapUserID)).Return(dbErr).Once()

	// The promoter mock fails the test if the second user is promoted.
	cmd := &BootstrapModerators{Promoter: promoter, Now: fixedClock}
	_, err := cmd.Execute(t.Context(), []string{bootstrapUserID, otherBootstrapUserID})

	require.ErrorIs(t, err, dbErr)
}
