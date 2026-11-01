package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestAPITokenList_ServeHTTP(t *testing.T) {
	t.Parallel()

	lastUsedAt := testNow.Add(30 * time.Minute)
	tokens := []domain.APIToken{
		{
			ID:         "6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55",
			UserID:     principalUserID,
			Prefix:     "0123abcd",
			Name:       new("CI runner"),
			CreatedAt:  testNow,
			LastUsedAt: &lastUsedAt,
		},
		{
			ID:        "0a8f5b3e-1c2d-4e5f-8a9b-0c1d2e3f4a5b",
			UserID:    principalUserID,
			Prefix:    "4567ef01",
			CreatedAt: testNow.Add(-time.Hour),
		},
	}

	cases := []struct {
		name       string
		anonymous  bool
		tokens     []domain.APIToken
		listErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "tokens",
			tokens:     tokens,
			wantStatus: http.StatusOK,
			wantBody: `{"data":[` +
				`{"id":"6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55","prefix":"0123abcd","name":"CI runner",` +
				`"created_at":"2026-10-31T12:00:00Z","last_used_at":"2026-10-31T12:30:00Z"},` +
				`{"id":"0a8f5b3e-1c2d-4e5f-8a9b-0c1d2e3f4a5b","prefix":"4567ef01","name":null,` +
				`"created_at":"2026-10-31T11:00:00Z","last_used_at":null}]}`,
		},
		{
			name:       "no_tokens",
			wantStatus: http.StatusOK,
			wantBody:   `{"data":[]}`,
		},
		{
			name:       listFailureCase,
			listErr:    errors.New("connection refused"),
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

			lister := mocks.NewActiveAPITokenLister(t)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/tokens", nil)
			if !tc.anonymous {
				req = asPrincipal(req)
				lister.EXPECT().ListUserActiveAPITokens(mock.Anything, principalUserID).Return(tc.tokens, tc.listErr)
			}

			rec := openapitest.New(t).Serve(t, APITokenList{Lister: lister}, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
