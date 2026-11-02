package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
)

func TestHealthz_ServeHTTP(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/healthz", nil)
	rec := openapitest.New(t).Serve(t, Healthz{}, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"status":"ok"}`, rec.Body.String())
}
