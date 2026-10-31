package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestAPITokenRevoke_ServeHTTP(t *testing.T) {
	t.Parallel()

	const revokeID = "6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55"

	cases := []struct {
		name       string
		anonymous  bool
		revokeErr  error
		wantStatus int
		wantBody   string
	}{
		{name: "revoked", wantStatus: http.StatusNoContent},
		{
			// The datasource reports unknown tokens, and tokens of other users, as not found.
			name:       "not_found",
			revokeErr:  datasources.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"code":"not_found","message":"API token not found"}`,
		},
		{
			name:       "revoke_failure",
			revokeErr:  errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
		{
			name:       anonymousCase,
			anonymous:  true,
			wantStatus: http.StatusUnauthorized,
			wantBody:   unauthenticatedBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			revoker := mocks.NewAPITokenRevoker(t)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/v1/tokens/"+revokeID, nil)
			req = mux.SetURLVars(req, map[string]string{"token_id": revokeID})
			if !tc.anonymous {
				req = asPrincipal(req)
				revoker.EXPECT().RevokeAPIToken(mock.Anything, principalUserID, revokeID, testNow).Return(tc.revokeErr)
			}

			rec := openapitest.New(t).Serve(t, APITokenRevoke{Revoker: revoker, Now: testClock}, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantBody == "" {
				assert.Empty(t, rec.Body.String())
			} else {
				assert.JSONEq(t, tc.wantBody, rec.Body.String())
			}
		})
	}
}
