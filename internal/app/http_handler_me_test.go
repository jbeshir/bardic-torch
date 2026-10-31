package app

import (
	"net/http"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/mock"
)

var meRoutes = []handlerRoute{
	{
		method:        http.MethodGet,
		path:          "/v1/me",
		successStatus: http.StatusOK,
		expectSuccess: func(handlerMocks, string) {},
	},
	{
		method:        http.MethodPatch,
		path:          "/v1/me",
		body:          `{"display_name":"  Morwen "}`,
		successStatus: http.StatusOK,
		expectSuccess: func(m handlerMocks, userID string) {
			m.displayNames.EXPECT().UpdateDisplayName(mock.Anything, userID, "Morwen").Return(nil)
			m.users.EXPECT().GetUser(mock.Anything, userID).Return(storedUser(userID), nil).Once()
		},
	},
}

func TestNewHTTPHandler_MeRoutesAuthorisation(t *testing.T) {
	t.Parallel()

	runAuthCases(t, meRoutes, []authCase{
		anonymousCase,
		unrecognisedCredentialsCase,
		invalidCredentialsCase,
		{name: "auth0_contributor", authorization: "Bearer auth0|" + contributorID, resolves: contributorID},
		{name: "auth0_moderator", authorization: "Bearer auth0|" + moderatorID, resolves: moderatorID},
		{
			name:          "api_token_contributor",
			authorization: "Bearer " + domain.APITokenPrefix + contributorID,
			resolves:      contributorID,
		},
		blockedCase,
		blockedAPITokenCase,
		unresolvableCase,
	})
}

func TestNewHTTPHandler_MeRoutesPreflightSkipsAuth(t *testing.T) {
	t.Parallel()

	assertPreflightSkipsAuth(t, meRoutes)
}
