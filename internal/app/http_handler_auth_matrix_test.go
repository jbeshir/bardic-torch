package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// principal is who a request is from, by the credentials it carries.
type principal struct {
	name          string
	authorization string
	// resolves is the user the auth middleware looks up for the request; empty for anonymous requests.
	resolves string
}

// Every principal the auth matrix sends requests as. The "API auth" names avoid gosec's G101, which flags
// identifiers containing "token".
var (
	anonymous = principal{name: "anonymous"}
	// unrecognisedCredentials carry no prefix any validator handles, so the request is anonymous.
	unrecognisedCredentials = principal{name: "unrecognised_credentials", authorization: "Bearer " + contributorID}
	invalidCredentials      = principal{name: "invalid_credentials", authorization: "Bearer auth0|" + invalidSecret}
	// unverifiableCredentials cannot be checked, as when the validator cannot reach Auth0.
	unverifiableCredentials = principal{
		name: "unverifiable_credentials", authorization: "Bearer auth0|" + unverifiableSecret,
	}
	auth0Contributor = principal{
		name: "auth0_contributor", authorization: "Bearer auth0|" + contributorID, resolves: contributorID,
	}
	auth0Moderator = principal{
		name: "auth0_moderator", authorization: "Bearer auth0|" + moderatorID, resolves: moderatorID,
	}
	apiAuthContributor = principal{
		name:          "api_token_contributor",
		authorization: "Bearer " + domain.APITokenPrefix + contributorID,
		resolves:      contributorID,
	}
	apiAuthModerator = principal{
		name:          "api_token_moderator",
		authorization: "Bearer " + domain.APITokenPrefix + moderatorID,
		resolves:      moderatorID,
	}
	blockedAuth0Contributor = principal{
		name: "auth0_blocked_contributor", authorization: "Bearer auth0|" + blockedID, resolves: blockedID,
	}
	blockedAPIAuthContributor = principal{
		name:          "api_token_blocked_contributor",
		authorization: "Bearer " + domain.APITokenPrefix + blockedID,
		resolves:      blockedID,
	}
	// unresolvable authenticates as a user whose lookup fails.
	unresolvable = principal{
		name: "unresolvable_user", authorization: "Bearer auth0|" + unresolvableID, resolves: unresolvableID,
	}
)

var principals = []principal{
	anonymous,
	unrecognisedCredentials,
	invalidCredentials,
	unverifiableCredentials,
	auth0Contributor,
	auth0Moderator,
	apiAuthContributor,
	apiAuthModerator,
	blockedAuth0Contributor,
	blockedAPIAuthContributor,
	unresolvable,
}

// outcome is the response a principal gets: the operation's success, or an error status and code.
type outcome struct {
	status int
	code   httpjson.ErrorCode
}

var (
	allowed           = outcome{}
	unauthenticated   = outcome{status: http.StatusUnauthorized, code: httpjson.CodeUnauthenticated}
	userBlocked       = outcome{status: http.StatusForbidden, code: httpjson.CodeUserBlocked}
	apiAuthNotAllowed = outcome{status: http.StatusForbidden, code: httpjson.CodeAPIAuthNotAllowed}
	moderatorRequired = outcome{status: http.StatusForbidden, code: httpjson.CodeModeratorRequired}
	internalError     = outcome{status: http.StatusInternalServerError, code: httpjson.CodeInternal}
)

// access is who may use an operation: whether its requests are authenticated, and each principal's outcome.
type access struct {
	authenticated bool
	outcomes      map[principal]outcome
}

// The kinds of access operations require, one per authorisation middleware. Each lists every principal.
var (
	// public operations are not authenticated, so they answer whatever credentials a request carries.
	public = access{outcomes: map[principal]outcome{
		anonymous:                 allowed,
		unrecognisedCredentials:   allowed,
		invalidCredentials:        allowed,
		unverifiableCredentials:   allowed,
		auth0Contributor:          allowed,
		auth0Moderator:            allowed,
		apiAuthContributor:        allowed,
		apiAuthModerator:          allowed,
		blockedAuth0Contributor:   allowed,
		blockedAPIAuthContributor: allowed,
		unresolvable:              allowed,
	}}
	// authenticatedUser operations are behind requireAuth.
	authenticatedUser = access{authenticated: true, outcomes: map[principal]outcome{
		anonymous:                 unauthenticated,
		unrecognisedCredentials:   unauthenticated,
		invalidCredentials:        unauthenticated,
		unverifiableCredentials:   internalError,
		auth0Contributor:          allowed,
		auth0Moderator:            allowed,
		apiAuthContributor:        allowed,
		apiAuthModerator:          allowed,
		blockedAuth0Contributor:   userBlocked,
		blockedAPIAuthContributor: userBlocked,
		unresolvable:              internalError,
	}}
	// interactiveUser operations are behind requireNonAPIToken.
	interactiveUser = access{authenticated: true, outcomes: map[principal]outcome{
		anonymous:                 unauthenticated,
		unrecognisedCredentials:   unauthenticated,
		invalidCredentials:        unauthenticated,
		unverifiableCredentials:   internalError,
		auth0Contributor:          allowed,
		auth0Moderator:            allowed,
		apiAuthContributor:        apiAuthNotAllowed,
		apiAuthModerator:          apiAuthNotAllowed,
		blockedAuth0Contributor:   userBlocked,
		blockedAPIAuthContributor: userBlocked,
		unresolvable:              internalError,
	}}
	// moderator operations are behind requireModerator.
	moderator = access{authenticated: true, outcomes: map[principal]outcome{
		anonymous:                 unauthenticated,
		unrecognisedCredentials:   unauthenticated,
		invalidCredentials:        unauthenticated,
		unverifiableCredentials:   internalError,
		auth0Contributor:          moderatorRequired,
		auth0Moderator:            allowed,
		apiAuthContributor:        moderatorRequired,
		apiAuthModerator:          allowed,
		blockedAuth0Contributor:   userBlocked,
		blockedAPIAuthContributor: userBlocked,
		unresolvable:              internalError,
	}}
)

// matrixRow is an operation's access, and a request to it that succeeds when allowed.
type matrixRow struct {
	access access
	// pathParams are the path parameter values, percent-encoded into the path.
	pathParams    map[string]string
	body          string
	successStatus int
	// expectSuccess sets up the datasources for a successful request by userID.
	expectSuccess func(m handlerMocks, userID string)
}

// blockBody is the body of a request to block a user.
const blockBody = `{"blocked":true}`

// blockTargetID is the user the moderation operation acts on.
const blockTargetID = "google-oauth2|123"

const revokeTargetID = "6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55"

// authMatrix is the access every operation in the OpenAPI spec requires. TestRouter_AuthMatrix fails for an
// operation without a row, so each new endpoint adds one here.
var authMatrix = map[openapitest.Operation]matrixRow{
	{Method: http.MethodGet, Path: "/healthz"}: {
		access:        public,
		successStatus: http.StatusOK,
		expectSuccess: func(handlerMocks, string) {},
	},
	{Method: http.MethodGet, Path: "/readyz"}: {
		access:        public,
		successStatus: http.StatusOK,
		expectSuccess: func(m handlerMocks, _ string) {
			m.pinger.EXPECT().Ping(mock.Anything).Return(nil)
		},
	},
	{Method: http.MethodGet, Path: "/v1/me"}: {
		access:        authenticatedUser,
		successStatus: http.StatusOK,
		expectSuccess: func(handlerMocks, string) {},
	},
	{Method: http.MethodPatch, Path: "/v1/me"}: {
		access:        authenticatedUser,
		body:          `{"display_name":"  Morwen "}`,
		successStatus: http.StatusOK,
		expectSuccess: func(m handlerMocks, userID string) {
			m.displayNames.EXPECT().UpdateDisplayName(mock.Anything, userID, "Morwen").Return(nil)
			m.users.EXPECT().GetUser(mock.Anything, userID).Return(storedUser(userID), nil).Once()
		},
	},
	{Method: http.MethodPut, Path: "/v1/users/{user_id}/blocked"}: {
		access:        moderator,
		pathParams:    map[string]string{"user_id": blockTargetID},
		body:          blockBody,
		successStatus: http.StatusOK,
		expectSuccess: func(m handlerMocks, _ string) {
			m.blocker.EXPECT().BlockUser(mock.Anything, blockTargetID, handlerNow).Return(nil)
			m.users.EXPECT().GetUser(mock.Anything, blockTargetID).Return(storedUser(blockTargetID), nil)
		},
	},
	{Method: http.MethodPost, Path: "/v1/tokens"}: {
		access:        interactiveUser,
		body:          `{"name":"CI runner"}`,
		successStatus: http.StatusCreated,
		expectSuccess: func(m handlerMocks, userID string) {
			m.counter.EXPECT().CountUserActiveAPITokens(mock.Anything, userID).Return(0, nil)
			m.creator.EXPECT().CreateAPIToken(mock.Anything, mock.Anything).Return(nil)
		},
	},
	{Method: http.MethodGet, Path: "/v1/tokens"}: {
		access:        interactiveUser,
		successStatus: http.StatusOK,
		expectSuccess: func(m handlerMocks, userID string) {
			m.lister.EXPECT().ListUserActiveAPITokens(mock.Anything, userID).Return(nil, nil)
		},
	},
	{Method: http.MethodDelete, Path: "/v1/tokens/{token_id}"}: {
		access:        interactiveUser,
		pathParams:    map[string]string{"token_id": revokeTargetID},
		successStatus: http.StatusNoContent,
		expectSuccess: func(m handlerMocks, userID string) {
			m.revoker.EXPECT().RevokeAPIToken(mock.Anything, userID, revokeTargetID, handlerNow).Return(nil)
		},
	},
}

// TestRouter_AuthMatrix sends a request as every principal to every operation in the OpenAPI spec, through the
// production handler, and checks the status, the error code and the contract. It also checks that preflight
// requests to every operation get 204 without authenticating, whatever their credentials.
func TestRouter_AuthMatrix(t *testing.T) {
	t.Parallel()

	operations := openapitest.New(t).Operations()
	for op := range authMatrix {
		assert.Contains(t, operations, op, "authMatrix has a row for an operation the spec does not have")
	}

	for _, op := range operations {
		row, ok := authMatrix[op]
		if !assert.True(t, ok, "%s %s has no row in authMatrix", op.Method, op.Path) {
			continue
		}

		t.Run(op.Method+" "+op.Path, func(t *testing.T) {
			t.Parallel()

			path := expandPath(op.Path, row.pathParams)
			for _, p := range principals {
				want, ok := row.access.outcomes[p]
				if !assert.True(t, ok, "access has no outcome for %s", p.name) {
					continue
				}

				t.Run(p.name, func(t *testing.T) {
					t.Parallel()
					assertOutcome(t, op.Method, path, row, p, want)
				})
			}

			t.Run("preflight", func(t *testing.T) {
				t.Parallel()
				assertPreflightSkipsAuth(t, op.Method, path)
			})
		})
	}
}

// expandPath fills a path template's parameters with their percent-encoded values.
func expandPath(template string, params map[string]string) string {
	path := template
	for name, value := range params {
		path = strings.ReplaceAll(path, "{"+name+"}", url.PathEscape(value))
	}
	return path
}

func assertOutcome(t *testing.T, method, path string, row matrixRow, p principal, want outcome) {
	t.Helper()

	m := newHandlerMocks(t)
	if row.access.authenticated && p.resolves != "" {
		m.expectUser(p.resolves)
	}
	wantStatus := want.status
	if want == allowed {
		wantStatus = row.successStatus
		row.expectSuccess(m, p.resolves)
	}

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(row.body))
	if row.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if p.authorization != "" {
		req.Header.Set("Authorization", p.authorization)
	}
	rec := m.validate.Serve(t, m.handler, req)

	assert.Equal(t, wantStatus, rec.Code)
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))

	var body errorResponse
	if rec.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	}
	assert.Equal(t, want.code, body.Code)
}

// assertPreflightSkipsAuth checks that a preflight request for method on path gets 204 as every principal. The
// mocks fail the test if a request reaches authentication or a controller. Preflight requests are answered by
// CORS for every path, so they are not operations in the spec.
func assertPreflightSkipsAuth(t *testing.T, method, path string) {
	t.Helper()

	m := newHandlerMocks(t)
	for _, p := range principals {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, path, nil)
		req.Header.Set("Access-Control-Request-Method", method)
		if p.authorization != "" {
			req.Header.Set("Authorization", p.authorization)
		}
		rec := httptest.NewRecorder()
		m.handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusNoContent, rec.Code, p.name)
		assert.Equal(t, allowedMethods, rec.Header().Get("Access-Control-Allow-Methods"), p.name)
		assert.Empty(t, rec.Body.String(), p.name)
	}
}
