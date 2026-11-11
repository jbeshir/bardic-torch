package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
)

func TestReadyz_ServeHTTP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		pingErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "database_reachable",
			wantStatus: http.StatusOK,
			wantBody:   `{"status":"ok"}`,
		},
		{
			name:       "database_unreachable",
			pingErr:    errors.New("dial tcp: connection refused"),
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   `{"code":"unavailable","message":"database unavailable"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)
			rec := openapitest.New(t).Serve(t, Readyz{Pinger: &fakerepo.Repo{PingErr: tc.pingErr}}, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
