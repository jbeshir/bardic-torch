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

// The keys the scripted fetches in the key cache tests return, in order.
const (
	firstKeys  = "first keys"
	secondKeys = "second keys"
	thirdKeys  = "third keys"
)

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

// keyFetch is one result of a scripted key fetch. If release is not nil, the fetch returns only once it is
// closed.
type keyFetch struct {
	keys    any
	err     error
	release chan struct{}
}

// scriptedKeyFetches returns its results in order, one per fetch, failing the test if fetched more often.
type scriptedKeyFetches struct {
	t       *testing.T
	mu      sync.Mutex
	results []keyFetch
	calls   int
}

func (s *scriptedKeyFetches) fetch(context.Context) (any, error) {
	s.mu.Lock()
	if s.calls >= len(s.results) {
		s.mu.Unlock()
		s.t.Errorf("unexpected key fetch %d", s.calls+1)
		return nil, errors.New("unexpected key fetch")
	}
	result := s.results[s.calls]
	s.calls++
	s.mu.Unlock()

	if result.release != nil {
		<-result.release
	}
	return result.keys, result.err
}

func (s *scriptedKeyFetches) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newTestKeyCache(t *testing.T, results ...keyFetch) (*auth0KeyCache, *scriptedKeyFetches, *advancingClock) {
	t.Helper()

	fetches := &scriptedKeyFetches{t: t, results: results}
	clock := &advancingClock{now: testNow}
	return newAuth0KeyCache(fetches.fetch, clock.Now), fetches, clock
}

// requireKeysEventually requires that the cache returns want once a background refresh has finished.
func requireKeysEventually(t *testing.T, cache *auth0KeyCache, want any) {
	t.Helper()

	require.Eventually(t, func() bool {
		keys, err := cache.get(t.Context())
		return err == nil && keys == want
	}, 5*time.Second, time.Millisecond)
}

func TestAuth0KeyCache_FetchesOnceWithinTTL(t *testing.T) {
	t.Parallel()

	cache, fetches, clock := newTestKeyCache(t, keyFetch{keys: firstKeys})

	keys, err := cache.get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, firstKeys, keys)

	clock.Advance(auth0KeysCacheTTL - time.Nanosecond)
	keys, err = cache.get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, firstKeys, keys)
	assert.Equal(t, 1, fetches.Calls())
}

func TestAuth0KeyCache_ServesStaleKeysWhileOneRefreshRuns(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	cache, fetches, clock := newTestKeyCache(t, keyFetch{keys: firstKeys}, keyFetch{keys: secondKeys, release: release})

	_, err := cache.get(t.Context())
	require.NoError(t, err)
	clock.Advance(auth0KeysCacheTTL)

	// The refresh is held until released, so every request until then gets the stale keys and starts no
	// further refresh.
	for range 3 {
		keys, err := cache.get(t.Context())
		require.NoError(t, err)
		assert.Equal(t, firstKeys, keys)
	}
	close(release)
	requireKeysEventually(t, cache, secondKeys)

	// The refreshed keys are used for a full TTL.
	clock.Advance(auth0KeysCacheTTL - time.Nanosecond)
	keys, err := cache.get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, secondKeys, keys)
	assert.Equal(t, 2, fetches.Calls())
}

func TestAuth0KeyCache_FailedRefreshDropsKeys(t *testing.T) {
	t.Parallel()

	cache, fetches, clock := newTestKeyCache(t,
		keyFetch{keys: firstKeys},
		keyFetch{err: errors.New("issuer unreachable")},
		keyFetch{keys: thirdKeys},
	)

	_, err := cache.get(t.Context())
	require.NoError(t, err)
	clock.Advance(auth0KeysCacheTTL)

	// The failed refresh drops the stale keys, so a later request fetches them itself.
	requireKeysEventually(t, cache, thirdKeys)
	assert.Equal(t, 3, fetches.Calls())
}

func TestAuth0KeyCache_FailedFetchIsUnavailableKeysAndCachesNothing(t *testing.T) {
	t.Parallel()

	cache, fetches, _ := newTestKeyCache(t, keyFetch{err: errors.New("issuer unreachable")}, keyFetch{keys: secondKeys})

	keys, err := cache.get(t.Context())
	require.ErrorIs(t, err, errSigningKeysUnavailable)
	require.ErrorContains(t, err, "issuer unreachable")
	assert.Nil(t, keys)

	keys, err = cache.get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, secondKeys, keys)
	assert.Equal(t, 2, fetches.Calls())
}
