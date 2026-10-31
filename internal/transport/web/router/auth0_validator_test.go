package router

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
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
	// expiryNotSatisfied is part of the error for a token past its expiry.
	expiryNotSatisfied = `"exp" not satisfied`
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
		{
			// The JWT library reads a numeric string as a NumericDate. Only the issuer can sign such a token, and
			// the date it holds is still checked (see expired_string_expiry in TestAuth0Validator_InvalidTokens).
			name: "string_expiry",
			claims: withClaim(issuer.Claims(testSubject, testAudience), "exp",
				strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)),
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

// withoutHeader returns auth0test.Header() without name.
func withoutHeader(name string) map[string]any {
	header := auth0test.Header()
	delete(header, name)
	return header
}

// jsonSerialization returns the flattened JWS JSON serialization of a compact token.
func jsonSerialization(t *testing.T, compact string) string {
	t.Helper()

	parts := strings.Split(compact, ".")
	require.Len(t, parts, 3)
	encoded, err := json.Marshal(map[string]string{"protected": parts[0], "payload": parts[1], "signature": parts[2]})
	require.NoError(t, err)
	return string(encoded)
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

	// Each case names a part of the error it must fail with, so that it cannot pass by failing an earlier check.
	cases := []struct {
		name    string
		token   string
		wantErr string
	}{
		{
			name:    "wrong_audience",
			token:   auth0test.Sign(t, issuer.Key, claims("aud", []string{"https://other.example"})),
			wantErr: "audience",
		},
		{name: "no_audience", token: auth0test.Sign(t, issuer.Key, claims("aud", nil)), wantErr: "audience"},
		{
			name:    "wrong_issuer",
			token:   auth0test.Sign(t, issuer.Key, claims("iss", "https://other.example/")),
			wantErr: "does not match any expected issuer",
		},
		{
			// The issuer is the URL with its trailing slash, as Auth0 writes it.
			name:    "issuer_without_trailing_slash",
			token:   auth0test.Sign(t, issuer.Key, claims("iss", strings.TrimSuffix(issuer.URL.String(), "/"))),
			wantErr: "does not match any expected issuer",
		},
		{name: "no_issuer", token: auth0test.Sign(t, issuer.Key, claims("iss", nil)), wantErr: "no issuer"},
		{
			name:    "expired",
			token:   auth0test.Sign(t, issuer.Key, claims("exp", time.Now().Add(-time.Hour).Unix())),
			wantErr: expiryNotSatisfied,
		},
		{
			name:    "expired_beyond_clock_skew",
			token:   auth0test.Sign(t, issuer.Key, claims("exp", time.Now().Add(-2*time.Minute).Unix())),
			wantErr: expiryNotSatisfied,
		},
		{
			name:    "not_yet_valid",
			token:   auth0test.Sign(t, issuer.Key, claims("nbf", time.Now().Add(5*time.Minute).Unix())),
			wantErr: `"nbf" not satisfied`,
		},
		{
			name:    "not_yet_valid_beyond_clock_skew",
			token:   auth0test.Sign(t, issuer.Key, claims("nbf", time.Now().Add(2*time.Minute).Unix())),
			wantErr: `"nbf" not satisfied`,
		},
		{
			name:    "issued_in_the_future",
			token:   auth0test.Sign(t, issuer.Key, claims("iat", time.Now().Add(5*time.Minute).Unix())),
			wantErr: `"iat" not satisfied`,
		},
		{name: "no_expiry", token: auth0test.Sign(t, issuer.Key, claims("exp", nil)), wantErr: "no expiry"},
		{
			// Signed under the issuer's key ID by a key that is not the issuer's.
			name:    "bad_signature",
			token:   auth0test.Sign(t, otherKey, valid),
			wantErr: "verification error",
		},
		{name: "no_subject", token: auth0test.Sign(t, issuer.Key, claims("sub", nil)), wantErr: "no subject"},
		{
			name:    "client_credentials_grant",
			token:   auth0test.Sign(t, issuer.Key, claims("gty", "client-credentials")),
			wantErr: errApplicationToken.Error(),
		},
		{
			name:    "application_subject",
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
			// RS384 is a valid signature by the issuer's key, in an algorithm other than the one Auth0 uses.
			name:    "rs384",
			token:   auth0test.SignWithHeader(t, issuer.Key, withHeader("alg", "RS384"), valid),
			wantErr: `algorithm "RS384" is not allowed`,
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
			name:    "empty_key_id",
			token:   auth0test.SignWithHeader(t, issuer.Key, withHeader("kid", ""), valid),
			wantErr: "no key ID",
		},
		{
			name:    "key_id_not_a_string",
			token:   auth0test.SignWithHeader(t, issuer.Key, withHeader("kid", 5), valid),
			wantErr: "parsing token header",
		},
		{
			// Correctly signed, but requiring a header extension this validator does not implement.
			name:    "critical_header",
			token:   auth0test.SignWithHeader(t, issuer.Key, withHeader("crit", []string{"exp"}), valid),
			wantErr: "critical extensions",
		},
		{
			// Only the compact serialization is accepted, even for a correctly signed token.
			name:    "json_serialization",
			token:   jsonSerialization(t, auth0test.Sign(t, issuer.Key, valid)),
			wantErr: "decoding token header",
		},
		{name: "malformed", token: "not-a-jwt", wantErr: "decoding token header"},
		{
			name:    "malformed_header_json",
			token:   base64.RawURLEncoding.EncodeToString([]byte("not json")) + ".e30.",
			wantErr: "parsing token header",
		},
		{
			// A string expiry is read as the date it holds, so an expired one is still rejected.
			name:    "expired_string_expiry",
			token:   auth0test.Sign(t, issuer.Key, claims("exp", strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10))),
			wantErr: expiryNotSatisfied,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			validate, err := NewAuth0Validator(issuer.URL, testAudience)
			require.NoError(t, err)

			identity, err := validateAuthorization(t, validate, "Bearer auth0|"+tc.token)

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
	fourthKeys = "fourth keys"
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

// fetch returns the next scripted result, or the context's error if ctx has ended by then, as an HTTP fetch
// would.
func (s *scriptedKeyFetches) fetch(ctx context.Context) (any, error) {
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
	if err := ctx.Err(); err != nil {
		return nil, err
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
		keyFetch{err: errors.New("issuer still unreachable")},
		keyFetch{keys: fourthKeys},
	)

	_, err := cache.get(t.Context())
	require.NoError(t, err)
	clock.Advance(auth0KeysCacheTTL)

	// Once the refresh fails the stale keys are no longer served: the next request fetches them itself, and
	// sees that fetch fail.
	require.Eventually(t, func() bool {
		_, err := cache.get(t.Context())
		return errors.Is(err, errSigningKeysUnavailable)
	}, 5*time.Second, time.Millisecond)
	assert.Equal(t, 3, fetches.Calls())

	keys, err := cache.get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, fourthKeys, keys)
}

func TestAuth0KeyCache_RefreshOutlivesTheRequestThatStartsIt(t *testing.T) {
	t.Parallel()

	cache, fetches, clock := newTestKeyCache(t, keyFetch{keys: firstKeys}, keyFetch{keys: secondKeys})

	_, err := cache.get(t.Context())
	require.NoError(t, err)
	clock.Advance(auth0KeysCacheTTL)

	// A request whose client has gone starts the refresh; the scripted fetch fails if its context has ended.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	keys, err := cache.get(ctx)
	require.NoError(t, err)
	assert.Equal(t, firstKeys, keys)

	requireKeysEventually(t, cache, secondKeys)
	assert.Equal(t, 2, fetches.Calls())
}

// waitObservingContext is a context that reports when the cache starts waiting on it: auth0KeyCache.get reads
// Done only once it has joined the fetch in flight, to wait for it.
type waitObservingContext struct {
	context.Context
	once    sync.Once
	waiting chan struct{}
}

func newWaitObservingContext(ctx context.Context) *waitObservingContext {
	return &waitObservingContext{Context: ctx, waiting: make(chan struct{})}
}

func (c *waitObservingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

type keysResult struct {
	keys any
	err  error
}

// getConcurrently calls cache.get from n goroutines, returning once every call is waiting for a fetch. Each
// call's result is sent on the returned channel.
func getConcurrently(t *testing.T, cache *auth0KeyCache, n int) <-chan keysResult {
	t.Helper()

	results := make(chan keysResult, n)
	for range n {
		ctx := newWaitObservingContext(t.Context())
		go func() {
			keys, err := cache.get(ctx)
			results <- keysResult{keys: keys, err: err}
		}()
		<-ctx.waiting
	}
	return results
}

func TestAuth0KeyCache_ConcurrentFirstRequestsShareOneFetch(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	cache, fetches, _ := newTestKeyCache(t, keyFetch{keys: firstKeys, release: release})

	const requests = 10
	results := getConcurrently(t, cache, requests)
	close(release)

	for range requests {
		result := <-results
		require.NoError(t, result.err)
		assert.Equal(t, firstKeys, result.keys)
	}
	assert.Equal(t, 1, fetches.Calls())
}

func TestAuth0KeyCache_ConcurrentFirstRequestsShareOneFailedFetch(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	cache, fetches, _ := newTestKeyCache(t, keyFetch{err: errors.New("issuer unreachable"), release: release})

	const requests = 10
	results := getConcurrently(t, cache, requests)
	close(release)

	for range requests {
		result := <-results
		require.ErrorIs(t, result.err, errSigningKeysUnavailable)
		require.ErrorContains(t, result.err, "issuer unreachable")
		assert.Nil(t, result.keys)
	}
	assert.Equal(t, 1, fetches.Calls())
}

func TestAuth0KeyCache_CancelledRequestStopsWaitingButNotTheFetch(t *testing.T) {
	t.Parallel()

	release := make(chan struct{})
	cache, fetches, _ := newTestKeyCache(t, keyFetch{keys: firstKeys, release: release})

	// The first request starts the fetch, then its client goes away while the fetch is held.
	ctx, cancel := context.WithCancel(t.Context())
	observed := newWaitObservingContext(ctx)
	cancelled := make(chan error, 1)
	go func() {
		_, err := cache.get(observed)
		cancelled <- err
	}()
	<-observed.waiting
	cancel()

	err := <-cancelled
	require.ErrorIs(t, err, errSigningKeysUnavailable)
	require.ErrorIs(t, err, context.Canceled)

	// The fetch it started carries on for the requests still waiting; the scripted fetch fails if its context
	// has ended.
	results := getConcurrently(t, cache, 1)
	close(release)
	result := <-results
	require.NoError(t, result.err)
	assert.Equal(t, firstKeys, result.keys)
	assert.Equal(t, 1, fetches.Calls())
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
