package app

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/testutil/refusingmysql"
	"github.com/stretchr/testify/assert"
)

// TestNewHTTPHandler_TooManyConnectionsIsUnavailable checks that a public read whose MySQL server turns its
// connection away with "Error 1040: Too many connections", as a server at max_connections does, answers the declared
// 503 unavailable, as for a database that cannot be reached, rather than 500 internal.
func TestNewHTTPHandler_TooManyConnectionsIsUnavailable(t *testing.T) {
	t.Parallel()

	handler := NewHTTPHandler(repositoryHTTPDependencies(mysql.New(refusingmysql.DB(t)), nil,
		mocks.NewTurnstileVerifier(t), unlimitedRateLimits()))

	for _, path := range []string{termsPath, piecesPath + "/the-copper-kettle"} {
		rec := openapitest.New(t).Serve(t, handler,
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code, path)
		assert.JSONEq(t, `{"code":"unavailable","message":"the database is busy or cannot be reached; try again"}`,
			rec.Body.String(), path)
	}
}
