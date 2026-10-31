package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	readyzPath = "/readyz"
	// allowedMethods is the Access-Control-Allow-Methods value: every method a route is registered for.
	allowedMethods = "DELETE, GET, PATCH, POST, PUT"
)

func TestNewHTTPHandler_HealthChecksIgnoreCredentials(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		path       string
		pingErr    error
		wantStatus int
	}{
		{name: "healthz", path: "/healthz", wantStatus: http.StatusOK},
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

func TestNewHTTPHandler_Preflight(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(HTTPDependencies{Pinger: mocks.NewPinger(t)})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, readyzPath, nil)
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, allowedMethods, rec.Header().Get("Access-Control-Allow-Methods"))
}

func TestNewHTTPHandler_UnknownRoute(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(HTTPDependencies{Pinger: mocks.NewPinger(t)})

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing", nil))

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
}
