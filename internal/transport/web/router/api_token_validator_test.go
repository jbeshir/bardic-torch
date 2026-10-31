package router

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testAPIToken = "user_api|0123456789abcdef"
	// testLookupHash is `printf 'user_api|0123456789abcdef' | sha256sum`.
	testLookupHash = "df74d56c5a5b52715862a5862de54d234b5a4f18a68f1277d364cf1a86ee89ec"
	testStoredID   = "6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55"
	testUserID     = "google-oauth2|123"
)

var testNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func testClock() time.Time {
	return testNow
}

func activeTestToken() domain.APIToken {
	return domain.APIToken{ID: testStoredID, UserID: testUserID, Prefix: "01234567", CreatedAt: testNow.Add(-time.Hour)}
}

func TestAPITokenValidator_ValidTokenRecordsUse(t *testing.T) {
	t.Parallel()

	tokens := mocks.NewAPITokenByHashGetter(t)
	tokens.EXPECT().GetAPITokenByHash(mock.Anything, testLookupHash).Return(activeTestToken(), nil)
	lastUsed := mocks.NewAPITokenLastUsedUpdater(t)
	lastUsed.EXPECT().UpdateAPITokenLastUsed(mock.Anything, testStoredID, testNow).Return(nil)

	validate := NewAPITokenValidator(tokens, lastUsed, testClock)
	identity, err := validateAuthorization(t, validate, "Bearer "+testAPIToken)

	require.NoError(t, err)
	assert.Equal(t, &Identity{UserID: testUserID, Method: domain.AuthMethodAPIToken}, identity)
}

func TestAPITokenValidator_LastUsedUpdateErrorIsLoggedNotFatal(t *testing.T) {
	t.Parallel()

	tokens := mocks.NewAPITokenByHashGetter(t)
	tokens.EXPECT().GetAPITokenByHash(mock.Anything, testLookupHash).Return(activeTestToken(), nil)
	lastUsed := mocks.NewAPITokenLastUsedUpdater(t)
	lastUsed.EXPECT().UpdateAPITokenLastUsed(mock.Anything, testStoredID, testNow).Return(errors.New("deadlock"))

	var logs bytes.Buffer
	ctx := domain.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/tokens", nil)
	req.Header.Set("Authorization", "Bearer "+testAPIToken)

	identity, err := NewAPITokenValidator(tokens, lastUsed, testClock)(req)

	require.NoError(t, err)
	assert.Equal(t, &Identity{UserID: testUserID, Method: domain.AuthMethodAPIToken}, identity)
	assert.Contains(t, logs.String(), `"level":"WARN"`)
	assert.Contains(t, logs.String(), `"token_id":"`+testStoredID+`"`)
	assert.Contains(t, logs.String(), "deadlock")
}

func TestAPITokenValidator_RejectedTokens(t *testing.T) {
	t.Parallel()

	revokedAt := testNow.Add(-time.Minute)
	revoked := activeTestToken()
	revoked.RevokedAt = &revokedAt

	cases := []struct {
		name      string
		token     domain.APIToken
		lookupErr error
	}{
		{name: "unknown", lookupErr: datasources.ErrNotFound},
		{name: "revoked", token: revoked},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tokens := mocks.NewAPITokenByHashGetter(t)
			tokens.EXPECT().GetAPITokenByHash(mock.Anything, testLookupHash).Return(tc.token, tc.lookupErr)

			// The last-used mock fails the test if a rejected token's use is recorded.
			validate := NewAPITokenValidator(tokens, mocks.NewAPITokenLastUsedUpdater(t), testClock)
			identity, err := validateAuthorization(t, validate, "Bearer "+testAPIToken)

			require.ErrorIs(t, err, ErrInvalidCredentials)
			assert.Nil(t, identity)
		})
	}
}

func TestAPITokenValidator_LookupErrorIsNotInvalidCredentials(t *testing.T) {
	t.Parallel()

	dbErr := errors.New("connection refused")
	tokens := mocks.NewAPITokenByHashGetter(t)
	tokens.EXPECT().GetAPITokenByHash(mock.Anything, testLookupHash).Return(domain.APIToken{}, dbErr)

	validate := NewAPITokenValidator(tokens, mocks.NewAPITokenLastUsedUpdater(t), testClock)
	identity, err := validateAuthorization(t, validate, "Bearer "+testAPIToken)

	require.ErrorIs(t, err, dbErr)
	require.NotErrorIs(t, err, ErrInvalidCredentials)
	assert.Nil(t, identity)
}

func TestAPITokenValidator_NonMatchingPrefixIsNotApplicable(t *testing.T) {
	t.Parallel()

	// The mocks fail the test if the validator looks anything up.
	validate := NewAPITokenValidator(mocks.NewAPITokenByHashGetter(t), mocks.NewAPITokenLastUsedUpdater(t), testClock)

	for _, authorization := range []string{
		"",
		"Bearer auth0|eyJhbGciOiJSUzI1NiJ9",
		"Bearer 0123456789abcdef",
		"user_api|0123456789abcdef",
	} {
		identity, err := validateAuthorization(t, validate, authorization)

		require.NoError(t, err, authorization)
		assert.Nil(t, identity, authorization)
	}
}
