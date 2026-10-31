package app

import (
	"net/http"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/mock"
)

const revokeTargetID = "6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55"

var tokenRoutes = []handlerRoute{
	{
		method:        http.MethodPost,
		path:          "/v1/tokens",
		body:          `{"name":"CI runner"}`,
		successStatus: http.StatusCreated,
		expectSuccess: func(m handlerMocks, userID string) {
			m.counter.EXPECT().CountUserActiveAPITokens(mock.Anything, userID).Return(0, nil)
			m.creator.EXPECT().CreateAPIToken(mock.Anything, mock.Anything).Return(nil)
		},
	},
	{
		method:        http.MethodGet,
		path:          "/v1/tokens",
		successStatus: http.StatusOK,
		expectSuccess: func(m handlerMocks, userID string) {
			m.lister.EXPECT().ListUserActiveAPITokens(mock.Anything, userID).Return(nil, nil)
		},
	},
	{
		method:        http.MethodDelete,
		path:          "/v1/tokens/" + revokeTargetID,
		successStatus: http.StatusNoContent,
		expectSuccess: func(m handlerMocks, userID string) {
			m.revoker.EXPECT().RevokeAPIToken(mock.Anything, userID, revokeTargetID, handlerNow).Return(nil)
		},
	},
}

func TestNewHTTPHandler_APITokenRoutesAuthorisation(t *testing.T) {
	t.Parallel()

	runAuthCases(t, tokenRoutes, []authCase{
		anonymousCase,
		unrecognisedCredentialsCase,
		invalidCredentialsCase,
		{name: "auth0_contributor", authorization: "Bearer auth0|" + contributorID, resolves: contributorID},
		blockedCase,
		blockedAPITokenCase,
		{
			name:          "api_token_principal",
			authorization: "Bearer " + domain.APITokenPrefix + contributorID,
			resolves:      contributorID,
			wantStatus:    http.StatusForbidden,
			wantCode:      httpjson.CodeAPIAuthNotAllowed,
		},
		unresolvableCase,
	})
}

func TestNewHTTPHandler_APITokenRoutesPreflightSkipsAuth(t *testing.T) {
	t.Parallel()

	assertPreflightSkipsAuth(t, tokenRoutes)
}
