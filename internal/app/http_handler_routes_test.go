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
	moderatorID    = "google-oauth2|moderator"
	blockedID      = "google-oauth2|blocked"
	unresolvableID = "google-oauth2|unresolvable"
	invalidSecret  = "invalid"
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

// handlerMocks are the datasources behind a handler built with NewHTTPHandler, authenticating with
// prefixValidator for both Auth0 ("auth0|") and API tokens.
type handlerMocks struct {
	users        *mocks.UserGetter
	displayNames *mocks.DisplayNameUpdater
	counter      *mocks.ActiveAPITokenCounter
	creator      *mocks.APITokenCreator
	lister       *mocks.ActiveAPITokenLister
	revoker      *mocks.APITokenRevoker
	handler      http.Handler
	validate     *openapitest.Validator
}

func newHandlerMocks(t *testing.T) handlerMocks {
	m := handlerMocks{
		users:        mocks.NewUserGetter(t),
		displayNames: mocks.NewDisplayNameUpdater(t),
		counter:      mocks.NewActiveAPITokenCounter(t),
		creator:      mocks.NewAPITokenCreator(t),
		lister:       mocks.NewActiveAPITokenLister(t),
		revoker:      mocks.NewAPITokenRevoker(t),
		validate:     openapitest.New(t),
	}
	m.handler = NewHTTPHandler(HTTPDependencies{
		Pinger: mocks.NewPinger(t),
		AuthValidators: []router.AuthValidator{
			prefixValidator("auth0|", domain.AuthMethodAuth0),
			prefixValidator(domain.APITokenPrefix, domain.AuthMethodAPIToken),
		},
		UserGetter:            m.users,
		UserEnsurer:           mocks.NewUserEnsurer(t),
		DisplayNameUpdater:    m.displayNames,
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

// storedUser is the user with userID as the datasource holds it: a contributor, unless userID is moderatorID
// or blockedID.
func storedUser(userID string) domain.User {
	user := domain.User{ID: userID, DisplayName: "Morwen", Role: domain.RoleContributor, CreatedAt: handlerNow}
	switch userID {
	case moderatorID:
		user.Role = domain.RoleModerator
	case blockedID:
		blockedAt := handlerNow.Add(-time.Hour)
		user.BlockedAt = &blockedAt
	}
	return user
}

// expectUser expects the auth middleware to resolve userID, failing for unresolvableID.
func (m handlerMocks) expectUser(userID string) {
	if userID == unresolvableID {
		m.users.EXPECT().GetUser(mock.Anything, userID).Return(domain.User{}, errors.New("connection refused")).Once()
		return
	}
	m.users.EXPECT().GetUser(mock.Anything, userID).Return(storedUser(userID), nil).Once()
}

// handlerRoute is a request to one route, and what it takes for the request to succeed.
type handlerRoute struct {
	method        string
	path          string
	body          string
	successStatus int
	// expectSuccess sets up the datasources for a successful request by userID.
	expectSuccess func(m handlerMocks, userID string)
}

// authCase is a request's credentials and the response it should get. A zero wantStatus means the request
// succeeds as the user it resolves.
type authCase struct {
	name          string
	authorization string
	resolves      string
	wantStatus    int
	wantCode      httpjson.ErrorCode
}

// Cases whose outcome does not depend on the route.
var (
	anonymousCase = authCase{
		name:       "anonymous",
		wantStatus: http.StatusUnauthorized,
		wantCode:   httpjson.CodeUnauthenticated,
	}
	unrecognisedCredentialsCase = authCase{
		name:          "unrecognised_credentials",
		authorization: "Bearer " + contributorID,
		wantStatus:    http.StatusUnauthorized,
		wantCode:      httpjson.CodeUnauthenticated,
	}
	invalidCredentialsCase = authCase{
		name:          "invalid_credentials",
		authorization: "Bearer auth0|" + invalidSecret,
		wantStatus:    http.StatusUnauthorized,
		wantCode:      httpjson.CodeUnauthenticated,
	}
	blockedCase = authCase{
		name:          "auth0_blocked",
		authorization: "Bearer auth0|" + blockedID,
		resolves:      blockedID,
		wantStatus:    http.StatusForbidden,
		wantCode:      httpjson.CodeUserBlocked,
	}
	blockedAPITokenCase = authCase{
		name:          "api_token_blocked",
		authorization: "Bearer " + domain.APITokenPrefix + blockedID,
		resolves:      blockedID,
		wantStatus:    http.StatusForbidden,
		wantCode:      httpjson.CodeUserBlocked,
	}
	unresolvableCase = authCase{
		name:          "unresolvable_user",
		authorization: "Bearer auth0|" + unresolvableID,
		resolves:      unresolvableID,
		wantStatus:    http.StatusInternalServerError,
		wantCode:      httpjson.CodeInternal,
	}
)

// runAuthCases sends each case's request to each route through the production handler, checking the status,
// error code and contract.
func runAuthCases(t *testing.T, routes []handlerRoute, cases []authCase) {
	t.Helper()

	for _, route := range routes {
		for _, tc := range cases {
			t.Run(route.method+"_"+tc.name, func(t *testing.T) {
				t.Parallel()

				m := newHandlerMocks(t)
				if tc.resolves != "" {
					m.expectUser(tc.resolves)
				}
				wantStatus := tc.wantStatus
				if wantStatus == 0 {
					wantStatus = route.successStatus
					route.expectSuccess(m, tc.resolves)
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

// assertPreflightSkipsAuth checks that preflight requests to routes get 204 without authenticating, even with
// invalid credentials. The mocks fail the test if a request reaches authentication or a controller.
func assertPreflightSkipsAuth(t *testing.T, routes []handlerRoute) {
	t.Helper()

	m := newHandlerMocks(t)
	for _, route := range routes {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, route.path, nil)
		req.Header.Set("Access-Control-Request-Method", route.method)
		req.Header.Set("Authorization", "Bearer auth0|"+invalidSecret)
		rec := httptest.NewRecorder()
		m.handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code, route.path)
		assert.Equal(t, allowedMethods, rec.Header().Get("Access-Control-Allow-Methods"), route.path)
	}
}
