package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCORSMiddleware_SetsHeadersAndCallsNext(t *testing.T) {
	t.Parallel()

	handler := cors("GET, POST")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Authorization, Content-Type", rec.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "GET, POST", rec.Header().Get("Access-Control-Allow-Methods"))
}

func TestCORSMiddleware_PreflightReturns204WithoutCallingNext(t *testing.T) {
	t.Parallel()

	// next stands in for authentication, which would reject the credential-less preflight.
	handler := cors("GET")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/v1/anything", nil)
	req.Header.Set("Origin", "https://bardictorch.org")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	req.Header.Set("Access-Control-Request-Headers", "authorization")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "*", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "Authorization, Content-Type", rec.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "GET", rec.Header().Get("Access-Control-Allow-Methods"))
	assert.Empty(t, rec.Body.String())
}

func TestCORSMiddleware_RouteMethodsListsEachMethodOnce(t *testing.T) {
	t.Parallel()

	routes := []route{
		{method: http.MethodGet, path: "/a"},
		{method: http.MethodPost, path: "/a"},
		{method: http.MethodGet, path: "/b"},
		{method: http.MethodDelete, path: "/v1/c"},
	}

	assert.Equal(t, []string{http.MethodDelete, http.MethodGet, http.MethodPost}, routeMethods(routes))
}
