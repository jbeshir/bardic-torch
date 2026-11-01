package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The routing corpus pins how New answers every kind of request at the HTTP level, independent of the router
// implementation: which route a request reaches, path cleaning redirects, 404 and 405 answers for unrouted
// requests, encoded path segments, and whether authentication runs.

const (
	// routingModerator is the Authorization header the routing fixture authenticates as a moderator.
	routingModerator = "Bearer moderator"
	// routingInvalid is the Authorization header the routing fixture rejects as invalid credentials.
	routingInvalid = "Bearer invalid"

	routingModeratorID = "auth0|moderator"
	routingRevokeID    = "0b0e5a4c-6a43-4f8e-9d3f-1c2b3a4d5e6f"

	allRouteMethods = "DELETE, GET, PATCH, POST, PUT"

	jsonContentType = "application/json"
	unmatched       = "unmatched"

	healthyBody         = `{"status":"ok"}` + "\n"
	noRouteBody         = `{"code":"not_found","message":"no route matches the path"}` + "\n"
	notAllowedBody      = `{"code":"method_not_allowed","message":"method not allowed for this path"}` + "\n"
	authRequiredBody    = `{"code":"unauthenticated","message":"authentication required"}` + "\n"
	invalidCredsBody    = `{"code":"unauthenticated","message":"invalid credentials"}` + "\n"
	userNotFoundBody    = `{"code":"not_found","message":"user not found"}` + "\n"
	tokenNotFoundBody   = `{"code":"not_found","message":"API token not found"}` + "\n"
	blockedTemplate     = "/v1/users/{user_id}/blocked"
	revokeTemplate      = "/v1/tokens/{token_id}"
	blockedRequestBody  = `{"blocked":true}`
	routingTokenPath    = "/v1/tokens/" + routingRevokeID
	routingBlockedPath  = "/v1/users/auth0%7Cuser/blocked"
	routingUnknownV1    = "/v1/unknown"
	routingUnknownRoot  = "/unknown"
	routingHealthz      = "/healthz"
	routingMe           = "/v1/me"
	routingTokens       = "/v1/tokens"
	routingReadyz       = "/readyz"
	routingConnectHost  = "example.com:443"
	routingConnectRedir = "//example.com:443/"
)

// routingFixture records what the handlers New builds were asked to do for one request.
type routingFixture struct {
	mu sync.Mutex
	// authRan is whether an auth validator was consulted.
	authRan bool
	// pathValue is the user or token ID a command or datasource was given.
	pathValue string
}

func (f *routingFixture) validate(r *http.Request) (*Identity, error) {
	f.mu.Lock()
	f.authRan = true
	f.mu.Unlock()

	switch r.Header.Get("Authorization") {
	case routingModerator:
		return &Identity{UserID: routingModeratorID, Method: domain.AuthMethodAuth0}, nil
	case routingInvalid:
		return nil, fmt.Errorf("bad signature: %w", ErrInvalidCredentials)
	default:
		return nil, nil
	}
}

func (f *routingFixture) recordPathValue(value string) {
	f.mu.Lock()
	f.pathValue = value
	f.mu.Unlock()
}

func (*routingFixture) Ping(context.Context) error {
	return nil
}

func (f *routingFixture) Execute(_ context.Context, req command.SetUserBlockedRequest) (domain.User, error) {
	f.recordPathValue(req.UserID)
	return domain.User{}, datasources.ErrNotFound
}

func (f *routingFixture) RevokeAPIToken(_ context.Context, _, tokenID string, _ time.Time) error {
	f.recordPathValue(tokenID)
	return datasources.ErrNotFound
}

func (f *routingFixture) dependencies() Dependencies {
	moderator := testUser
	moderator.ID, moderator.Role = routingModeratorID, domain.RoleModerator
	return Dependencies{
		Pinger:          f,
		AuthValidators:  []AuthValidator{f.validate},
		ResolveUser:     resolveTo(moderator),
		SetUserBlocked:  f,
		APITokenRevoker: f,
		Now:             func() time.Time { return testNow },
	}
}

// routingCase is one request and everything observable about its answer. Empty strings are asserted as absent.
type routingCase struct {
	method        string
	target        string
	authorization string
	body          string

	status      int
	allow       string
	location    string
	contentType string
	respBody    string
	authRan     bool
	// route is the request log's route attribute.
	route string
	// pathValue is the ID the controller passed on, if it got that far.
	pathValue string
}

// routeRow is one path of the route table, used to generate a case for every method.
type routeRow struct {
	path string
	// allow is the Allow header of a 405 answer for the path.
	allow string
	// unroutedRoute is the logged route of requests for the path with a method it is not routed for, or of
	// OPTIONS requests; empty means unmatched.
	unroutedRoute string
	// routed are the answers to anonymous requests for each method the path is routed for.
	routed map[string]routingCase
}

var everyMethod = []string{
	http.MethodDelete, http.MethodGet, http.MethodHead, http.MethodOptions,
	http.MethodPatch, http.MethodPost, http.MethodPut,
}

func anonymousAuthRequired(template string) routingCase {
	return routingCase{
		status: http.StatusUnauthorized, contentType: jsonContentType, respBody: authRequiredBody,
		authRan: true, route: template,
	}
}

var routeRows = []routeRow{
	{path: routingHealthz, allow: "GET", routed: map[string]routingCase{
		http.MethodGet: {status: http.StatusOK, contentType: jsonContentType, respBody: healthyBody, route: routingHealthz},
	}},
	{path: routingReadyz, allow: "GET", routed: map[string]routingCase{
		http.MethodGet: {status: http.StatusOK, contentType: jsonContentType, respBody: healthyBody, route: routingReadyz},
	}},
	{path: routingMe, allow: "GET, PATCH", routed: map[string]routingCase{
		http.MethodGet:   anonymousAuthRequired(routingMe),
		http.MethodPatch: anonymousAuthRequired(routingMe),
	}},
	{path: routingBlockedPath, allow: "PUT", routed: map[string]routingCase{
		http.MethodPut: anonymousAuthRequired(blockedTemplate),
	}},
	{path: routingTokens, allow: "GET, POST", routed: map[string]routingCase{
		http.MethodGet:  anonymousAuthRequired(routingTokens),
		http.MethodPost: anonymousAuthRequired(routingTokens),
	}},
	// gorilla/mux quirk: the last /v1 route clears the subrouter's method mismatch, so a wrong method matches the
	// /v1 prefix route itself, and the 405 probe finds every method.
	{path: routingTokenPath, allow: allRouteMethods, unroutedRoute: "/v1", routed: map[string]routingCase{
		http.MethodDelete: anonymousAuthRequired(revokeTemplate),
	}},
}

// everyRouteAndMethod sends every method, anonymously, to every route: routed methods reach the route, OPTIONS
// is answered by CORS and every other method, HEAD included, gets 405.
func everyRouteAndMethod() []routingCase {
	var cases []routingCase
	for _, row := range routeRows {
		for _, method := range everyMethod {
			tc, routed := row.routed[method]
			unroutedRoute := unmatched
			if row.unroutedRoute != "" {
				unroutedRoute = row.unroutedRoute
			}
			switch {
			case method == http.MethodOptions:
				tc = routingCase{status: http.StatusNoContent, route: unroutedRoute}
			case !routed:
				tc = methodNotAllowed(row.allow)
				tc.route = unroutedRoute
			}
			tc.method, tc.target = method, row.path
			cases = append(cases, tc)
		}
	}
	return cases
}

func notFound(method, target, authorization string) routingCase {
	return routingCase{
		method: method, target: target, authorization: authorization,
		status: http.StatusNotFound, contentType: jsonContentType, respBody: noRouteBody, route: unmatched,
	}
}

func methodNotAllowed(allow string) routingCase {
	return routingCase{
		status: http.StatusMethodNotAllowed, allow: allow, contentType: jsonContentType, respBody: notAllowedBody,
		route: unmatched,
	}
}

func notAllowedAt(method, target, authorization, allow string) routingCase {
	tc := methodNotAllowed(allow)
	tc.method, tc.target, tc.authorization = method, target, authorization
	return tc
}

func redirect(method, target, location string) routingCase {
	return routingCase{
		method: method, target: target, status: http.StatusMovedPermanently, location: location, route: unmatched,
	}
}

func blockAsModerator(target, wantUserID string) routingCase {
	return routingCase{
		method: http.MethodPut, target: target, authorization: routingModerator, body: blockedRequestBody,
		status: http.StatusNotFound, contentType: jsonContentType, respBody: userNotFoundBody, authRan: true,
		route: blockedTemplate, pathValue: wantUserID,
	}
}

func revokeAsModerator(target string) routingCase {
	return routingCase{
		method: http.MethodDelete, target: target, authorization: routingModerator,
		status: http.StatusNotFound, contentType: jsonContentType, respBody: tokenNotFoundBody, authRan: true,
		route: revokeTemplate,
	}
}

// unroutedCases are requests no route matches. None of them authenticates, so invalid credentials do not
// turn them into 401s.
func unroutedCases() []routingCase {
	return []routingCase{
		notFound(http.MethodGet, routingUnknownRoot, ""),
		notFound(http.MethodPost, routingUnknownRoot, routingInvalid),
		notFound(http.MethodGet, "/v1", ""),
		notFound(http.MethodGet, "/v1/", ""),
		notFound(http.MethodGet, routingUnknownV1, ""),
		notFound(http.MethodGet, routingUnknownV1, routingInvalid),
		notFound(http.MethodDelete, routingUnknownV1, routingInvalid),
		notFound(http.MethodGet, "/v1me", routingInvalid),
		notFound(http.MethodGet, "/v1/tokens/"+routingRevokeID+"/extra", routingInvalid),
		notAllowedAt(http.MethodDelete, routingMe, routingInvalid, "GET, PATCH"),
		notAllowedAt(http.MethodHead, routingMe, routingInvalid, "GET, PATCH"),
		notAllowedAt(http.MethodPut, routingTokens, routingInvalid, "GET, POST"),
		{
			method: http.MethodGet, target: routingTokenPath, authorization: routingInvalid,
			status: http.StatusMethodNotAllowed, allow: allRouteMethods, contentType: jsonContentType,
			respBody: notAllowedBody, route: "/v1",
		},
		notAllowedAt(http.MethodGet, routingBlockedPath, routingInvalid, "PUT"),
		notAllowedAt(http.MethodPost, routingHealthz, routingInvalid, "GET"),
		notAllowedAt(http.MethodConnect, routingHealthz, "", "GET"),
		notAllowedAt("PROPFIND", routingHealthz, "", "GET"),
		// Trailing slashes are not cleaned away, and no route has one.
		notFound(http.MethodGet, "/healthz/", ""),
		notFound(http.MethodGet, "/v1/me/", routingInvalid),
		notFound(http.MethodGet, "/v1/tokens/", routingInvalid),
		notFound(http.MethodPost, "/v1/tokens/", routingInvalid),
		notFound(http.MethodDelete, "/v1/tokens/", routingInvalid),
		// Literal path segments match only as sent: an encoded letter is a different path.
		notFound(http.MethodGet, "/health%7A", ""),
		notFound(http.MethodGet, "/%68ealthz", ""),
		notFound(http.MethodGet, "/v1/%6De", routingInvalid),
		notFound(http.MethodGet, "/%761/me", routingInvalid),
		notFound(http.MethodPut, "/v1/users/auth0%7Cuser/bl%6Fcked", routingInvalid),
		notFound(http.MethodDelete, "/v1/tok%65ns/"+routingRevokeID, routingInvalid),
	}
}

// cleaningCases are paths that are not in canonical form. They are redirected to the cleaned path, keeping the
// query, for every method and before any routing or authentication.
func cleaningCases() []routingCase {

	return []routingCase{
		redirect(http.MethodGet, "//healthz", routingHealthz),
		redirect(http.MethodGet, "//healthz?a=1&b=%2F", "/healthz?a=1&b=%2F"),
		redirect(http.MethodPost, "//healthz", routingHealthz),
		redirect(http.MethodHead, "//healthz", routingHealthz),
		redirect(http.MethodGet, "/./healthz", routingHealthz),
		redirect(http.MethodGet, "/v1/../healthz?x=1", "/healthz?x=1"),
		redirect(http.MethodGet, "/v1//me", routingMe),
		redirect(http.MethodGet, "/v1//me?q=%7C", "/v1/me?q=%7C"),
		redirect(http.MethodPatch, "/v1/./me", routingMe),
		redirect(http.MethodGet, "/v1/me//", "/v1/me/"),
		redirect(http.MethodGet, "/v1//unknown", routingUnknownV1),
		redirect(http.MethodGet, "/unknown/../..", "/"),
		redirect(http.MethodPut, "/v1/users//blocked", "/v1/users/blocked"),
		// The cleaned path is set as the URL's decoded path, so its escapes are escaped again.
		redirect(http.MethodPut, "/v1/users/a%2Fb//blocked?x=1", "/v1/users/a%252Fb/blocked?x=1"),
		redirect(http.MethodGet, "*", "/%2A"),
		// A CONNECT request in authority form has an empty path, which cleans to "/".
		redirect(http.MethodConnect, routingConnectHost, routingConnectRedir),
		redirect(http.MethodPut, "/v1/users//blocked/", "/v1/users/blocked/"),
		{
			method: http.MethodPut, target: "/v1/users/./blocked", authorization: routingInvalid,
			status: http.StatusMovedPermanently, location: "/v1/users/blocked", route: blockedTemplate,
		},
	}
}

// encodedIDCases send IDs with percent-encoded characters. Each path segment is decoded once: %2F stays inside
// its segment, and %257C reaches the controller as %7C.
func encodedIDCases() []routingCase {
	return []routingCase{
		blockAsModerator("/v1/users/auth0%7Cuser/blocked", "auth0|user"),
		blockAsModerator("/v1/users/auth0%7ca%2Fb/blocked", "auth0|a/b"),
		blockAsModerator("/v1/users/auth0%257Cuser/blocked", "auth0%7Cuser"),
		blockAsModerator("/v1/users/a%2E%2E/blocked", "a.."),
		blockAsModerator("/v1/users/%2E%2E/blocked", ".."),
		blockAsModerator("/v1/users/%2Fx/blocked", "/x"),
		blockAsModerator("/v1/users/%2F/blocked", "/"),
		revokeAsModerator("/v1/tokens/%2F"),
		revokeAsModerator("/v1/tokens/a%2Fb"),
		{
			method: http.MethodDelete, target: routingTokenPath, authorization: routingModerator,
			status: http.StatusNotFound, contentType: jsonContentType, respBody: tokenNotFoundBody, authRan: true,
			route: revokeTemplate, pathValue: routingRevokeID,
		},
		{
			method: http.MethodGet, target: routingMe, authorization: routingInvalid,
			status: http.StatusUnauthorized, contentType: jsonContentType, respBody: invalidCredsBody, authRan: true,
			route: routingMe,
		},
		{
			method: http.MethodGet, target: "/healthz?x=%7C", authorization: routingInvalid,
			status: http.StatusOK, contentType: jsonContentType, respBody: healthyBody, route: routingHealthz,
		},
	}
}

// TestRouter_Routing sends the routing corpus through New and checks every observable part of each answer.
func TestRouter_Routing(t *testing.T) {
	t.Parallel()

	var cases []routingCase
	cases = append(cases, everyRouteAndMethod()...)
	cases = append(cases, unroutedCases()...)
	cases = append(cases, cleaningCases()...)
	cases = append(cases, encodedIDCases()...)

	for _, tc := range cases {
		name := tc.method + " " + tc.target
		if tc.authorization != "" {
			name += " as " + strings.TrimPrefix(tc.authorization, "Bearer ")
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			rec, fixture, route := serveRouting(t, tc)

			assert.Equal(t, tc.status, rec.Code, "status")
			assert.Equal(t, tc.allow, rec.Header().Get("Allow"), "Allow")
			assert.Equal(t, tc.location, rec.Header().Get("Location"), "Location")
			assert.Equal(t, tc.contentType, rec.Header().Get("Content-Type"), "Content-Type")
			assert.Equal(t, allRouteMethods, rec.Header().Get("Access-Control-Allow-Methods"),
				"Access-Control-Allow-Methods")
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"), "security headers")
			assert.Equal(t, tc.respBody, rec.Body.String(), "body")
			assert.Equal(t, tc.authRan, fixture.authRan, "whether authentication ran")
			assert.Equal(t, tc.pathValue, fixture.pathValue, "path value passed on")
			assert.Equal(t, tc.route, route, "logged route")
		})
	}
}

// serveRouting serves tc's request through New with a routing fixture, returning the response, the fixture and
// the route attribute of the request's completion log.
func serveRouting(t *testing.T, tc routingCase) (*httptest.ResponseRecorder, *routingFixture, string) {
	t.Helper()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))

	req := httptest.NewRequestWithContext(domain.ContextWithLogger(t.Context(), logger), tc.method, tc.target,
		strings.NewReader(tc.body))
	if tc.body != "" {
		req.Header.Set("Content-Type", jsonContentType)
	}
	if tc.authorization != "" {
		req.Header.Set("Authorization", tc.authorization)
	}

	fixture := &routingFixture{}
	rec := httptest.NewRecorder()
	New(fixture.dependencies()).ServeHTTP(rec, req)

	var route any
	for line := range strings.Lines(logs.String()) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		if entry["msg"] == "request completed" {
			route = entry["route"]
		}
	}
	routeString, ok := route.(string)
	require.True(t, ok, "the request completion log has a route")

	return rec, fixture, routeString
}
