package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

			pinger := mocks.NewPinger(t)
			pinger.EXPECT().
				Ping(mock.MatchedBy(func(ctx context.Context) bool {
					deadline, ok := ctx.Deadline()
					return ok && time.Until(deadline) <= readyzPingTimeout
				})).
				Return(tc.pingErr)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/readyz", nil)
			rec := openapitest.New(t).Serve(t, Readyz{Pinger: pinger}, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
