package router

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestAuth0Validator_ValidTokens(t *testing.T) {
	t.Parallel()

	issuer := auth0test.NewIssuer(t)

	cases := []struct {
		name   string
		claims map[string]any
	}{
		{name: "fresh", claims: issuer.Claims(testSubject, testAudience)},
		{
			// Expiry is allowed auth0ClockSkew of leeway for clocks that disagree.
			name:   "expired_within_clock_skew",
			claims: withClaim(issuer.Claims(testSubject, testAudience), "exp", time.Now().Add(-30*time.Second).Unix()),
		},
		{
			name:   "not_before_within_clock_skew",
			claims: withClaim(issuer.Claims(testSubject, testAudience), "nbf", time.Now().Add(30*time.Second).Unix()),
		},
		{
			name:   "authorization_code_grant",
			claims: withClaim(issuer.Claims(testSubject, testAudience), "gty", "authorization_code"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			validate, err := NewAuth0Validator(issuer.URL, testAudience)
			require.NoError(t, err)

			identity, err := validateAuthorization(t, validate, "Bearer auth0|"+auth0test.Sign(t, issuer.Key, tc.claims))

			require.NoError(t, err)
			assert.Equal(t, &Identity{UserID: testSubject, Method: domain.AuthMethodAuth0}, identity)
		})
	}
}

// withClaim returns claims with name set to value, or removed if value is nil.
func withClaim(claims map[string]any, name string, value any) map[string]any {
	if value == nil {
		delete(claims, name)
	} else {
		claims[name] = value
	}
	return claims
}

// withHeader returns auth0test.Header() with name set to value.
func withHeader(name string, value any) map[string]any {
	header := auth0test.Header()
	header[name] = value
	return header
}

func TestAuth0Validator_InvalidTokens(t *testing.T) {
	t.Parallel()

	issuer := auth0test.NewIssuer(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&issuer.Key.PublicKey)
	require.NoError(t, err)
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER})

	claims := func(name string, value any) map[string]any {
		return withClaim(issuer.Claims(testSubject, testAudience), name, value)
	}
	valid := issuer.Claims(testSubject, testAudience)

	cases := []struct {
		name  string
		token string
	}{
		{name: "wrong_audience", token: auth0test.Sign(t, issuer.Key, claims("aud", []string{"https://other.example"}))},
		{name: "wrong_issuer", token: auth0test.Sign(t, issuer.Key, claims("iss", "https://other.example/"))},
		{name: "expired", token: auth0test.Sign(t, issuer.Key, claims("exp", time.Now().Add(-time.Hour).Unix()))},
		{
			name:  "expired_beyond_clock_skew",
			token: auth0test.Sign(t, issuer.Key, claims("exp", time.Now().Add(-2*time.Minute).Unix())),
		},
		{
			name:  "not_yet_valid",
			token: auth0test.Sign(t, issuer.Key, claims("nbf", time.Now().Add(5*time.Minute).Unix())),
		},
		{name: "no_expiry", token: auth0test.Sign(t, issuer.Key, claims("exp", nil))},
		{name: "bad_signature", token: auth0test.Sign(t, otherKey, valid)},
		{name: "no_subject", token: auth0test.Sign(t, issuer.Key, claims("sub", nil))},
		{
			name:  "client_credentials_grant",
			token: auth0test.Sign(t, issuer.Key, claims("gty", "client-credentials")),
		},
		{
			name:  "application_subject",
			token: auth0test.Sign(t, issuer.Key, claims("sub", "uP7lW0cQx3BxgP1Bt6OY2MNs4eGvTj6R@clients")),
		},
		{name: "alg_none", token: auth0test.SignWithHeader(t, nil, withHeader("alg", "none"), valid)},
		{
			// The classic algorithm confusion: an HMAC keyed with the issuer's public key, which anyone has.
			name:  "hs256_with_public_key",
			token: auth0test.SignWithHeader(t, publicKeyPEM, withHeader("alg", "HS256"), valid),
		},
		{name: "unknown_key_id", token: auth0test.SignWithHeader(t, issuer.Key, withHeader("kid", "other-key"), valid)},
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

func TestAuth0Validator_OversizedTokenIsRejectedBeforeParsing(t *testing.T) {
	t.Parallel()

	issuer := auth0test.NewIssuer(t)
	validate, err := NewAuth0Validator(issuer.URL, testAudience)
	require.NoError(t, err)

	// The token is correctly signed, so only its length rejects it.
	padded := withClaim(issuer.Claims(testSubject, testAudience), "padding", strings.Repeat("a", maxAuth0TokenBytes))
	token := auth0test.Sign(t, issuer.Key, padded)
	require.Greater(t, len(token), maxAuth0TokenBytes)

	for _, token := range []string{token, strings.Repeat(".", maxAuth0TokenBytes+1)} {
		identity, err := validateAuthorization(t, validate, "Bearer auth0|"+token)

		require.ErrorIs(t, err, ErrInvalidCredentials)
		require.ErrorContains(t, err, "longer than")
		assert.Nil(t, identity)
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
