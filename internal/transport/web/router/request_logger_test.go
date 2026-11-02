package router

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveLogged serves req through requestLogger wrapping a router for routes, with a JSON logger in the request context, and
// returns the decoded log entries.
func serveLogged(t *testing.T, routes []route, req *http.Request) (*httptest.ResponseRecorder, []map[string]any) {
	t.Helper()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	req = req.WithContext(domain.ContextWithLogger(req.Context(), logger))

	router := newRouter(routes, authMiddleware(nil, nil))
	rec := httptest.NewRecorder()
	requestLogger(router.lookup)(router).ServeHTTP(rec, req)

	var entries []map[string]any
	for line := range strings.Lines(logs.String()) {
		var entry map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &entry))
		entries = append(entries, entry)
	}

	return rec, entries
}

func TestRequestLogger_LogsStatusAndRoute(t *testing.T) {
	t.Parallel()

	routes := []route{{method: http.MethodGet, path: "/songs/{song_id}", handler: http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			domain.LoggerFromContext(r.Context()).InfoContext(r.Context(), "handling")
			w.WriteHeader(http.StatusTeapot)
			_, _ = w.Write([]byte("body"))
			w.WriteHeader(http.StatusInternalServerError)
		},
	)}}

	rec, entries := serveLogged(t, routes,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/songs/123", nil))

	assert.Equal(t, http.StatusTeapot, rec.Code)
	require.Len(t, entries, 2)

	handling, completed := entries[0], entries[1]
	assert.Equal(t, "handling", handling["msg"])
	assert.Equal(t, "request completed", completed["msg"])

	for _, entry := range entries {
		assert.Equal(t, http.MethodGet, entry["method"])
		assert.Equal(t, "/songs/{song_id}", entry["route"])
		assert.NotEmpty(t, entry["request_id"])
	}
	assert.Equal(t, handling["request_id"], completed["request_id"],
		"handler logs must share the request ID of the completion log")
	assert.InDelta(t, http.StatusTeapot, completed["status"], 0)
	assert.Contains(t, completed, "duration")
}

func TestRequestLogger_DefaultStatusAndUnmatchedRoute(t *testing.T) {
	t.Parallel()

	routes := []route{{method: http.MethodGet, path: "/healthz", handler: http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		},
	)}}

	_, entries := serveLogged(t, routes,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	require.Len(t, entries, 1)
	assert.InDelta(t, http.StatusOK, entries[0]["status"], 0)

	_, entries = serveLogged(t, routes,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing/123", nil))
	require.Len(t, entries, 1)
	assert.Equal(t, unmatchedRoute, entries[0]["route"])
	assert.InDelta(t, http.StatusNotFound, entries[0]["status"], 0)
}

func TestRequestLogger_RequestIDsAreUnique(t *testing.T) {
	t.Parallel()

	routes := []route{{method: http.MethodGet, path: "/healthz", handler: http.NotFoundHandler()}}

	_, first := serveLogged(t, routes, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	_, second := serveLogged(t, routes, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil))
	require.Len(t, first, 1)
	require.Len(t, second, 1)
	assert.NotEqual(t, first[0]["request_id"], second[0]["request_id"])
}
