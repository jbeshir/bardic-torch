package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	healthzPath = "/healthz"
	readyzPath  = "/readyz"
	mePath      = "/v1/me"
	tokensPath  = "/v1/tokens"
	termsPath   = "/v1/terms"
	eventsPath  = "/v1/events"
	slugParam   = "slug"
	// methodNotAllowedBody is the response to a method no route at the path accepts.
	methodNotAllowedBody = `{"code":"method_not_allowed","message":"method not allowed for this path"}`
	// noRouteBody is the response to a path no route matches.
	noRouteBody = `{"code":"not_found","message":"no route matches the path"}`
)

func TestNewHTTPHandler_HealthChecksIgnoreCredentials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		path       string
		pingErr    error
		wantStatus int
	}{
		{name: "healthz", path: healthzPath, wantStatus: http.StatusOK},
		{name: "readyz_ready", path: readyzPath, wantStatus: http.StatusOK},
		{name: "readyz_unavailable", path: readyzPath, pingErr: errors.New("down"), wantStatus: http.StatusServiceUnavailable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			pinger := mocks.NewPinger(t)
			if tc.path == readyzPath {
				pinger.EXPECT().Ping(mock.Anything).Return(tc.pingErr)
			}
			handler := NewHTTPHandler(HTTPDependencies{Pinger: pinger})

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, tc.path, nil)
			req.Header.Set("Authorization", "Bearer auth0|not-a-valid-token")
			rec := openapitest.New(t).Serve(t, handler, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}

func TestNewHTTPHandler_UnroutedRequests(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name          string
		method        string
		path          string
		authorization string
		wantStatus    int
		wantAllow     string
		wantBody      string
	}{
		{
			name:       "unknown_path",
			method:     http.MethodGet,
			path:       "/missing",
			wantStatus: http.StatusNotFound,
			wantBody:   noRouteBody,
		},
		{
			name:          "unknown_v1_path_with_credentials",
			method:        http.MethodGet,
			path:          "/v1/missing",
			authorization: "Bearer auth0|" + invalidSecret,
			wantStatus:    http.StatusNotFound,
			wantBody:      noRouteBody,
		},
		{
			name:       "wrong_method_on_health_check",
			method:     http.MethodPost,
			path:       healthzPath,
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  http.MethodGet,
			wantBody:   methodNotAllowedBody,
		},
		{
			name:       "wrong_method_on_me",
			method:     http.MethodDelete,
			path:       mePath,
			wantStatus: http.StatusMethodNotAllowed,
			wantAllow:  "GET, PATCH",
			wantBody:   methodNotAllowedBody,
		},
		{
			// The method is checked before credentials, so even invalid ones get 405 rather than 401.
			name:          "wrong_method_on_tokens_with_credentials",
			method:        http.MethodPut,
			path:          tokensPath,
			authorization: "Bearer auth0|" + invalidSecret,
			wantStatus:    http.StatusMethodNotAllowed,
			wantAllow:     "GET, POST",
			wantBody:      methodNotAllowedBody,
		},
		{
			// Catalogue reads are routed outside the /v1 subrouter, whose unrouted handler still finds them.
			name:          "wrong_method_on_piece_with_credentials",
			method:        http.MethodDelete,
			path:          "/v1/pieces/the-copper-kettle",
			authorization: "Bearer auth0|" + invalidSecret,
			wantStatus:    http.StatusMethodNotAllowed,
			wantAllow:     http.MethodGet,
			wantBody:      methodNotAllowedBody,
		},
		{
			name:       "catalogue_path_with_extra_segment",
			method:     http.MethodGet,
			path:       "/v1/persons/wren/extra",
			wantStatus: http.StatusNotFound,
			wantBody:   noRouteBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Invalid credentials would get 401 from authentication, and the mocks fail the test if a controller runs.
			m := newHandlerMocks(t)
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.path, nil)
			if tc.authorization != "" {
				req.Header.Set("Authorization", tc.authorization)
			}
			rec := httptest.NewRecorder()
			m.handler.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.Equal(t, tc.wantAllow, rec.Header().Get("Allow"))
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
			assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}

func TestNewHTTPHandler_PreflightToUnknownPath(t *testing.T) {
	t.Parallel()

	m := newHandlerMocks(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/v1/missing", nil)
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()
	m.handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, allowedMethods(m.validate), rec.Header().Get("Access-Control-Allow-Methods"))
	assert.Empty(t, rec.Body.String())
}

// allowedMethods is the Access-Control-Allow-Methods value: every method of an operation in spec, which
// TestRouter_RoutesMatchOpenAPI checks are the routed ones.
func allowedMethods(spec *openapitest.Validator) string {
	var methods []string
	for _, op := range spec.Operations() {
		methods = append(methods, op.Method)
	}
	slices.Sort(methods)
	return strings.Join(slices.Compact(methods), ", ")
}
