package router

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

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

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/me", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}

	return validate(req)
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

// withoutHeader returns auth0test.Header() without name.
func withoutHeader(name string) map[string]any {
	header := auth0test.Header()
	delete(header, name)
	return header
}

func TestAuth0Validator_ValidTokenGivesItsSubject(t *testing.T) {
	t.Parallel()

	issuer := auth0test.NewIssuer(t)
	validate, err := NewAuth0Validator(issuer.URL, testAudience)
	require.NoError(t, err)

	identity, err := validateAuthorization(t, validate,
		"Bearer "+auth0test.Sign(t, issuer.Key, issuer.Claims(testSubject, testAudience)))

	require.NoError(t, err)
	assert.Equal(t, &Identity{UserID: testSubject}, identity)
}

// TestAuth0Validator_InvalidTokens has one case per rule the validator enforces on a token.
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

	// Each case names a part of the error it must fail with, so that it cannot pass by failing an earlier check.
	cases := []struct {
		name    string
		token   string
		wantErr string
	}{
		{
			// Signed under the issuer's key ID by a key that is not the issuer's.
			name:    "bad_signature",
			token:   auth0test.Sign(t, otherKey, valid),
			wantErr: "verification error",
		},
		{
			name:    "wrong_issuer",
			token:   auth0test.Sign(t, issuer.Key, claims("iss", "https://other.example/")),
			wantErr: "does not match any expected issuer",
		},
		{
			name:    "wrong_audience",
			token:   auth0test.Sign(t, issuer.Key, claims("aud", []string{"https://other.example"})),
			wantErr: "audience",
		},
		{
			name:    "expired",
			token:   auth0test.Sign(t, issuer.Key, claims("exp", time.Now().Add(-time.Hour).Unix())),
			wantErr: `"exp" not satisfied`,
		},
		{name: "no_expiry", token: auth0test.Sign(t, issuer.Key, claims("exp", nil)), wantErr: "no expiry"},
		{name: "no_subject", token: auth0test.Sign(t, issuer.Key, claims("sub", nil)), wantErr: "no subject"},
		{
			// Tokens Auth0 issues to applications are not users, whether marked by the grant type or the subject.
			name:    "application_token_by_grant",
			token:   auth0test.Sign(t, issuer.Key, claims("gty", "client-credentials")),
			wantErr: errApplicationToken.Error(),
		},
		{
			name:    "application_token_by_subject",
			token:   auth0test.Sign(t, issuer.Key, claims("sub", "uP7lW0cQx3BxgP1Bt6OY2MNs4eGvTj6R@clients")),
			wantErr: errApplicationToken.Error(),
		},
		{
			name:    "alg_none",
			token:   auth0test.SignWithHeader(t, nil, withHeader("alg", "none"), valid),
			wantErr: `algorithm "none" is not allowed`,
		},
		{
			// The classic algorithm confusion: an HMAC keyed with the issuer's public key, which anyone has.
			name:    "hs256_with_public_key",
			token:   auth0test.SignWithHeader(t, publicKeyPEM, withHeader("alg", "HS256"), valid),
			wantErr: `algorithm "HS256" is not allowed`,
		},
		{
			name:    "unknown_key_id",
			token:   auth0test.SignWithHeader(t, issuer.Key, withHeader("kid", "other-key"), valid),
			wantErr: `key ID "other-key"`,
		},
		{
			// Correctly signed, but naming no key; the JWKS has a single key the library would otherwise use.
			name:    "no_key_id",
			token:   auth0test.SignWithHeader(t, issuer.Key, withoutHeader("kid"), valid),
			wantErr: "no key ID",
		},
		{
			// Correctly signed, but requiring a header extension this validator does not implement.
			name:    "critical_header",
			token:   auth0test.SignWithHeader(t, issuer.Key, withHeader("crit", []string{"exp"}), valid),
			wantErr: "critical extensions",
		},
		{name: "malformed", token: "not-a-jwt", wantErr: "decoding token header"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			validate, err := NewAuth0Validator(issuer.URL, testAudience)
			require.NoError(t, err)

			identity, err := validateAuthorization(t, validate, "Bearer "+tc.token)

			require.ErrorIs(t, err, ErrInvalidCredentials)
			require.ErrorContains(t, err, tc.wantErr)
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

	identity, err := validateAuthorization(t, validate, "Bearer "+token)

	require.ErrorIs(t, err, ErrInvalidCredentials)
	require.ErrorContains(t, err, "longer than")
	assert.Nil(t, identity)
}

func TestAuth0Validator_NonBearerHeaderIsNotApplicable(t *testing.T) {
	t.Parallel()

	issuer := auth0test.NewIssuer(t)
	validate, err := NewAuth0Validator(issuer.URL, testAudience)
	require.NoError(t, err)
	token := auth0test.Sign(t, issuer.Key, issuer.Claims(testSubject, testAudience))

	for _, authorization := range []string{"", "Basic " + token} {
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
	identity, err := validateAuthorization(t, validate, "Bearer "+auth0test.Sign(t, issuer.Key, claims))

	require.ErrorIs(t, err, errSigningKeysUnavailable)
	require.NotErrorIs(t, err, ErrInvalidCredentials)
	assert.Nil(t, identity)
}

// scriptedKeyFetches returns its keys in order, one per fetch, and counts the fetches.
type scriptedKeyFetches struct {
	mu    sync.Mutex
	keys  []any
	calls int
}

func (s *scriptedKeyFetches) fetch(context.Context) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.calls >= len(s.keys) {
		return nil, errors.New("unexpected key fetch")
	}
	s.calls++
	return s.keys[s.calls-1], nil
}

func (s *scriptedKeyFetches) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// advancingClock is a clock the test moves forward, safe to read from the key cache's refresh goroutine.
type advancingClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *advancingClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *advancingClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestAuth0KeyCache_FetchesOnceWithinTTLAndAgainAfterIt(t *testing.T) {
	t.Parallel()

	const firstKeys, secondKeys = "first keys", "second keys"
	fetches := &scriptedKeyFetches{keys: []any{firstKeys, secondKeys}}
	clock := &advancingClock{now: testNow}
	cache := newAuth0KeyCache(fetches.fetch, clock.Now)

	for _, age := range []time.Duration{0, auth0KeysCacheTTL - time.Nanosecond} {
		clock.Advance(age)
		keys, err := cache.get(t.Context())
		require.NoError(t, err)
		assert.Equal(t, firstKeys, keys)
	}
	assert.Equal(t, 1, fetches.Calls())

	// Past the TTL a request still gets the old keys while one refresh replaces them.
	clock.Advance(time.Nanosecond)
	require.Eventually(t, func() bool {
		keys, err := cache.get(t.Context())
		return err == nil && keys == secondKeys
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, 2, fetches.Calls())
}
