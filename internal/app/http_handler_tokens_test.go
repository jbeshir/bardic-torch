package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var handlerNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// Users the fake validators authenticate as, by the ID after their credential prefix.
const (
	contributorID  = "google-oauth2|contributor"
	blockedID      = "google-oauth2|blocked"
	unresolvableID = "google-oauth2|unresolvable"
	invalidSecret  = "invalid"
	revokeTargetID = "6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55"
	createBody     = `{"name":"CI runner"}`
)

// prefixValidator authenticates "Bearer <prefix><user ID>" as the user ID with method, and rejects
// "Bearer <prefix>invalid".
func prefixValidator(prefix string, method domain.AuthMethod) router.AuthValidator {
	return func(r *http.Request) (*router.Identity, error) {
		userID, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "+prefix)
		switch {
		case !ok:
			return nil, nil
		case userID == invalidSecret:
			return nil, fmt.Errorf("%w: test credential", router.ErrInvalidCredentials)
		default:
			return &router.Identity{UserID: userID, Method: method}, nil
		}
	}
}

// tokenHandlerMocks are the datasources behind NewHTTPHandler for the API token routes.
type tokenHandlerMocks struct {
	users    *mocks.UserGetter
	counter  *mocks.ActiveAPITokenCounter
	creator  *mocks.APITokenCreator
	lister   *mocks.ActiveAPITokenLister
	revoker  *mocks.APITokenRevoker
	handler  http.Handler
	validate *openapitest.Validator
}

func newTokenHandlerMocks(t *testing.T) tokenHandlerMocks {
	m := tokenHandlerMocks{
		users:    mocks.NewUserGetter(t),
		counter:  mocks.NewActiveAPITokenCounter(t),
		creator:  mocks.NewAPITokenCreator(t),
		lister:   mocks.NewActiveAPITokenLister(t),
		revoker:  mocks.NewAPITokenRevoker(t),
		validate: openapitest.New(t),
	}
	m.handler = NewHTTPHandler(HTTPDependencies{
		Pinger: mocks.NewPinger(t),
		AuthValidators: []router.AuthValidator{
			prefixValidator("auth0|", domain.AuthMethodAuth0),
			prefixValidator(domain.APITokenPrefix, domain.AuthMethodAPIToken),
		},
		UserGetter:            m.users,
		UserEnsurer:           mocks.NewUserEnsurer(t),
		APITokenCreator:       m.creator,
		ActiveAPITokenCounter: m.counter,
		ActiveAPITokenLister:  m.lister,
		APITokenRevoker:       m.revoker,
		Now: func() time.Time {
			return handlerNow
		},
	})
	return m
}

func (m tokenHandlerMocks) expectUser(userID string) {
	user := domain.User{ID: userID, DisplayName: "Morwen", Role: domain.RoleContributor, CreatedAt: handlerNow}
	switch userID {
	case blockedID:
		blockedAt := handlerNow.Add(-time.Hour)
		user.BlockedAt = &blockedAt
	case unresolvableID:
		m.users.EXPECT().GetUser(mock.Anything, userID).Return(domain.User{}, errors.New("connection refused"))
		return
	}
	m.users.EXPECT().GetUser(mock.Anything, userID).Return(user, nil)
}

type tokenRoute struct {
	method        string
	path          string
	body          string
	successStatus int
	expectSuccess func(m tokenHandlerMocks)
}

var tokenRoutes = []tokenRoute{
	{
		method:        http.MethodPost,
		path:          "/v1/tokens",
		body:          createBody,
		successStatus: http.StatusCreated,
		expectSuccess: func(m tokenHandlerMocks) {
			m.counter.EXPECT().CountUserActiveAPITokens(mock.Anything, contributorID).Return(0, nil)
			m.creator.EXPECT().CreateAPIToken(mock.Anything, mock.Anything).Return(nil)
		},
	},
	{
		method:        http.MethodGet,
		path:          "/v1/tokens",
		successStatus: http.StatusOK,
		expectSuccess: func(m tokenHandlerMocks) {
			m.lister.EXPECT().ListUserActiveAPITokens(mock.Anything, contributorID).Return(nil, nil)
		},
	},
	{
		method:        http.MethodDelete,
		path:          "/v1/tokens/" + revokeTargetID,
		successStatus: http.StatusNoContent,
		expectSuccess: func(m tokenHandlerMocks) {
			m.revoker.EXPECT().RevokeAPIToken(mock.Anything, contributorID, revokeTargetID, handlerNow).Return(nil)
		},
	},
}

func TestNewHTTPHandler_APITokenRoutesAuthorisation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		authorization string
		resolves      string
		wantStatus    int
		wantCode      httpjson.ErrorCode
	}{
		{name: "anonymous", wantStatus: http.StatusUnauthorized, wantCode: httpjson.CodeUnauthenticated},
		{
			name:          "unrecognised_credentials",
			authorization: "Bearer " + contributorID,
			wantStatus:    http.StatusUnauthorized,
			wantCode:      httpjson.CodeUnauthenticated,
		},
		{
			name:          "invalid_credentials",
			authorization: "Bearer auth0|" + invalidSecret,
			wantStatus:    http.StatusUnauthorized,
			wantCode:      httpjson.CodeUnauthenticated,
		},
		{name: "auth0_contributor", authorization: "Bearer auth0|" + contributorID, resolves: contributorID},
		{
			name:          "auth0_blocked",
			authorization: "Bearer auth0|" + blockedID,
			resolves:      blockedID,
			wantStatus:    http.StatusForbidden,
			wantCode:      httpjson.CodeUserBlocked,
		},
		{
			name:          "api_token_principal",
			authorization: "Bearer " + domain.APITokenPrefix + contributorID,
			resolves:      contributorID,
			wantStatus:    http.StatusForbidden,
			wantCode:      httpjson.CodeAPIAuthNotAllowed,
		},
		{
			name:          "unresolvable_user",
			authorization: "Bearer auth0|" + unresolvableID,
			resolves:      unresolvableID,
			wantStatus:    http.StatusInternalServerError,
			wantCode:      httpjson.CodeInternal,
		},
	}

	for _, route := range tokenRoutes {
		for _, tc := range cases {
			t.Run(route.method+"_"+tc.name, func(t *testing.T) {
				t.Parallel()

				m := newTokenHandlerMocks(t)
				if tc.resolves != "" {
					m.expectUser(tc.resolves)
				}
				wantStatus := tc.wantStatus
				if wantStatus == 0 {
					wantStatus = route.successStatus
					route.expectSuccess(m)
				}

				req := httptest.NewRequestWithContext(t.Context(), route.method, route.path, strings.NewReader(route.body))
				if route.body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				if tc.authorization != "" {
					req.Header.Set("Authorization", tc.authorization)
				}
				rec := m.validate.Serve(t, m.handler, req)

				assert.Equal(t, wantStatus, rec.Code)
				assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
				if tc.wantCode != "" {
					var body struct {
						Code httpjson.ErrorCode `json:"code"`
					}
					require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
					assert.Equal(t, tc.wantCode, body.Code)
				}
			})
		}
	}
}

func TestNewHTTPHandler_APITokenRoutesPreflightSkipsAuth(t *testing.T) {
	t.Parallel()

	// The mocks fail the test if a preflight request reaches authentication or a controller.
	m := newTokenHandlerMocks(t)

	for _, route := range tokenRoutes {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, route.path, nil)
		req.Header.Set("Access-Control-Request-Method", route.method)
		req.Header.Set("Authorization", "Bearer auth0|"+invalidSecret)
		rec := httptest.NewRecorder()
		m.handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code, route.path)
		assert.Equal(t, "DELETE, GET, POST", rec.Header().Get("Access-Control-Allow-Methods"), route.path)
	}
}
