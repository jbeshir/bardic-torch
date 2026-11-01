package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestAPITokenRevoke_ServeHTTP(t *testing.T) {
	t.Parallel()

	const (
		revokeID     = "6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55"
		notFoundBody = `{"code":"not_found","message":"API token not found"}`
	)

	cases := []struct {
		name      string
		anonymous bool
		// pathID is the token_id path value as the router passes it on, decoded; empty means revokeID.
		pathID string
		// revokes says whether the datasource is asked to revoke revokeID.
		revokes    bool
		revokeErr  error
		wantStatus int
		wantBody   string
	}{
		{name: "revoked", revokes: true, wantStatus: http.StatusNoContent},
		{
			// The datasource reports unknown tokens, and tokens of other users, as not found.
			name:       "not_found",
			revokes:    true,
			revokeErr:  datasources.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   notFoundBody,
		},
		{name: "not_a_uuid", pathID: "token", wantStatus: http.StatusNotFound, wantBody: notFoundBody},
		{name: "trailing_space", pathID: revokeID + " ", wantStatus: http.StatusNotFound, wantBody: notFoundBody},
		{
			name:       "uppercase_uuid",
			pathID:     "6F1C1F0E-8A4B-4F43-9A5E-2D7C0B1E3A55",
			wantStatus: http.StatusNotFound,
			wantBody:   notFoundBody,
		},
		{
			name:       "unhyphenated_uuid",
			pathID:     "6f1c1f0e8a4b4f439a5e2d7c0b1e3a55",
			wantStatus: http.StatusNotFound,
			wantBody:   notFoundBody,
		},
		{
			name:       "revoke_failure",
			revokes:    true,
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

			pathID := revokeID
			if tc.pathID != "" {
				pathID = tc.pathID
			}

			revoker := mocks.NewAPITokenRevoker(t)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/v1/tokens/"+revokeID, nil)
			req.SetPathValue("token_id", pathID)
			if !tc.anonymous {
				req = asPrincipal(req)
			}
			if tc.revokes {
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
