package router

import (
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/auth0test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAudience = "https://api.bardictorch.org"
	testSubject  = "google-oauth2|123"
)

func validateAuthorization(t *testing.T, validate AuthValidator, authorization string) (*Identity, error) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/tokens", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}

	return validate(req)
}

func TestAuth0Validator_ValidToken(t *testing.T) {
	t.Parallel()

	issuer := auth0test.NewIssuer(t)
	validate, err := NewAuth0Validator(issuer.URL, testAudience)
	require.NoError(t, err)

	identity, err := validateAuthorization(t, validate, "Bearer auth0|"+auth0test.Sign(t, issuer.Key, issuer.Claims(testSubject, testAudience)))

	require.NoError(t, err)
	assert.Equal(t, &Identity{UserID: testSubject, Method: domain.AuthMethodAuth0}, identity)
}

func TestAuth0Validator_InvalidTokens(t *testing.T) {
	t.Parallel()

	issuer := auth0test.NewIssuer(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	withClaim := func(name string, value any) map[string]any {
		claims := issuer.Claims(testSubject, testAudience)
		if value == nil {
			delete(claims, name)
		} else {
			claims[name] = value
		}
		return claims
	}

	cases := []struct {
		name  string
		token string
	}{
		{name: "wrong_audience", token: auth0test.Sign(t, issuer.Key, withClaim("aud", []string{"https://other.example"}))},
		{name: "wrong_issuer", token: auth0test.Sign(t, issuer.Key, withClaim("iss", "https://other.example/"))},
		{name: "expired", token: auth0test.Sign(t, issuer.Key, withClaim("exp", time.Now().Add(-time.Hour).Unix()))},
		{name: "bad_signature", token: auth0test.Sign(t, otherKey, issuer.Claims(testSubject, testAudience))},
		{name: "no_subject", token: auth0test.Sign(t, issuer.Key, withClaim("sub", nil))},
		{name: "malformed", token: "not-a-jwt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			validate, err := NewAuth0Validator(issuer.URL, testAudience)
			require.NoError(t, err)

			identity, err := validateAuthorization(t, validate, "Bearer auth0|"+tc.token)

			require.ErrorIs(t, err, ErrInvalidCredentials)
			assert.Nil(t, identity)
		})
	}
}

func TestAuth0Validator_NonMatchingPrefixIsNotApplicable(t *testing.T) {
	t.Parallel()

	issuer := auth0test.NewIssuer(t)
	validate, err := NewAuth0Validator(issuer.URL, testAudience)
	require.NoError(t, err)
	token := auth0test.Sign(t, issuer.Key, issuer.Claims(testSubject, testAudience))

	for _, authorization := range []string{
		"",
		"Bearer " + token,
		"Bearer user_api|" + token,
		"Basic auth0|" + token,
	} {
		identity, err := validateAuthorization(t, validate, authorization)

		require.NoError(t, err, authorization)
		assert.Nil(t, identity, authorization)
	}
}

func TestAuth0Validator_UnavailableKeysAreNotInvalidCredentials(t *testing.T) {
	t.Parallel()

	issuer := auth0test.NewIssuer(t)
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	unreachableIssuer, err := url.Parse(server.URL + "/")
	require.NoError(t, err)

	validate, err := NewAuth0Validator(unreachableIssuer, testAudience)
	require.NoError(t, err)

	claims := issuer.Claims(testSubject, testAudience)
	claims["iss"] = unreachableIssuer.String()
	identity, err := validateAuthorization(t, validate, "Bearer auth0|"+auth0test.Sign(t, issuer.Key, claims))

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrInvalidCredentials)
	assert.Nil(t, identity)
}
