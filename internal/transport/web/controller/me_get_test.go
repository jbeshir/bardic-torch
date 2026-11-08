package controller

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
)

func TestMeGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		anonymous  bool
		wantStatus int
		wantBody   string
	}{
		{
			name:       "principal",
			wantStatus: http.StatusOK,
			wantBody:   `{"id":"google-oauth2|123","public_id":"k3vq7xw2mbnc4zra","display_name":"Morwen","role":"contributor"}`,
		},
		{name: anonymousCase, anonymous: true, wantStatus: http.StatusUnauthorized, wantBody: unauthenticatedBody},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/me", nil)
			if !tc.anonymous {
				req = asPrincipal(req)
			}
			rec := openapitest.New(t).Serve(t, MeGet{}, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
