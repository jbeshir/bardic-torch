package router

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAudience = "https://api.bardictorch.org"
	testKeyID    = "test-key"
	testSubject  = "google-oauth2|123"
)

// auth0Issuer is a local stand-in for an Auth0 tenant: it serves the OpenID discovery document and a JWKS with
// one RSA signing key, and signs tokens with that key.
type auth0Issuer struct {
	url *url.URL
	key *rsa.PrivateKey
}

func newAuth0Issuer(t *testing.T) auth0Issuer {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]string{"jwks_uri": server.URL + "/.well-known/jwks.json"})
	})
	mux.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, _ *http.Request) {
		writeTestJSON(t, w, map[string]any{"keys": []map[string]string{{
			"kty": "RSA",
			"use": "sig",
			"alg": "RS256",
			"kid": testKeyID,
			"n":   base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})

	issuerURL, err := url.Parse(server.URL + "/")
	require.NoError(t, err)

	return auth0Issuer{url: issuerURL, key: key}
}

func writeTestJSON(t *testing.T, w http.ResponseWriter, body any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	assert.NoError(t, json.NewEncoder(w).Encode(body))
}

// validClaims returns the claims of a token the issuer's validator accepts.
func (i auth0Issuer) validClaims() map[string]any {
	now := time.Now()
	return map[string]any{
		"iss": i.url.String(),
		"sub": testSubject,
		"aud": []string{testAudience},
		"iat": now.Unix(),
		"exp": now.Add(time.Hour).Unix(),
	}
}

// signRS256 returns a compact RS256 JWS of claims, signed with key under the issuer's key ID.
func signRS256(t *testing.T, key *rsa.PrivateKey, claims map[string]any) string {
	t.Helper()

	encode := func(v any) string {
		b, err := json.Marshal(v)
		require.NoError(t, err)
		return base64.RawURLEncoding.EncodeToString(b)
	}

	signingInput := encode(map[string]string{"alg": "RS256", "typ": "JWT", "kid": testKeyID}) + "." + encode(claims)
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	require.NoError(t, err)

	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
}

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

	issuer := newAuth0Issuer(t)
	validate, err := NewAuth0Validator(issuer.url, testAudience)
	require.NoError(t, err)

	identity, err := validateAuthorization(t, validate, "Bearer auth0|"+signRS256(t, issuer.key, issuer.validClaims()))

	require.NoError(t, err)
	assert.Equal(t, &Identity{UserID: testSubject, Method: domain.AuthMethodAuth0}, identity)
}

func TestAuth0Validator_InvalidTokens(t *testing.T) {
	t.Parallel()

	issuer := newAuth0Issuer(t)
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	withClaim := func(name string, value any) map[string]any {
		claims := issuer.validClaims()
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
		{name: "wrong_audience", token: signRS256(t, issuer.key, withClaim("aud", []string{"https://other.example"}))},
		{name: "wrong_issuer", token: signRS256(t, issuer.key, withClaim("iss", "https://other.example/"))},
		{name: "expired", token: signRS256(t, issuer.key, withClaim("exp", time.Now().Add(-time.Hour).Unix()))},
		{name: "bad_signature", token: signRS256(t, otherKey, issuer.validClaims())},
		{name: "no_subject", token: signRS256(t, issuer.key, withClaim("sub", nil))},
		{name: "malformed", token: "not-a-jwt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			validate, err := NewAuth0Validator(issuer.url, testAudience)
			require.NoError(t, err)

			identity, err := validateAuthorization(t, validate, "Bearer auth0|"+tc.token)

			require.ErrorIs(t, err, ErrInvalidCredentials)
			assert.Nil(t, identity)
		})
	}
}

func TestAuth0Validator_NonMatchingPrefixIsNotApplicable(t *testing.T) {
	t.Parallel()

	issuer := newAuth0Issuer(t)
	validate, err := NewAuth0Validator(issuer.url, testAudience)
	require.NoError(t, err)
	token := signRS256(t, issuer.key, issuer.validClaims())

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

	issuer := newAuth0Issuer(t)
	server := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(server.Close)
	unreachableIssuer, err := url.Parse(server.URL + "/")
	require.NoError(t, err)

	validate, err := NewAuth0Validator(unreachableIssuer, testAudience)
	require.NoError(t, err)

	claims := issuer.validClaims()
	claims["iss"] = unreachableIssuer.String()
	identity, err := validateAuthorization(t, validate, "Bearer auth0|"+signRS256(t, issuer.key, claims))

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrInvalidCredentials)
	assert.Nil(t, identity)
}
