package router

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/auth0/go-jwt-middleware/v3/jwks"
	"github.com/auth0/go-jwt-middleware/v3/validator"
)

const (
	auth0HeaderPrefix = "Bearer "
	// maxAuth0TokenBytes bounds the tokens that are parsed at all. Auth0 access tokens are well under 2 KiB; the
	// JWT library splits its input on "." more than once while checking a token, so an unbounded token costs
	// memory in proportion to its length before it is rejected.
	maxAuth0TokenBytes = 8 << 10
	// auth0KeysCacheTTL is how long the issuer's signing keys are used before they are fetched again.
	auth0KeysCacheTTL = 5 * time.Minute
	// auth0KeysFetchTimeout bounds each fetch of the issuer's discovery document and signing keys.
	auth0KeysFetchTimeout = 10 * time.Second
	// auth0ClockSkew is how far token time claims may be off from this server's clock.
	auth0ClockSkew = time.Minute
	// auth0ClientCredentialsGrant is the gty claim of tokens Auth0 issues to applications rather than users.
	auth0ClientCredentialsGrant = "client-credentials"
	// auth0ClientSubjectSuffix ends the subject of tokens Auth0 issues to applications: "<client_id>@clients".
	auth0ClientSubjectSuffix = "@clients"
)

var (
	// errSigningKeysUnavailable marks a failure to fetch the issuer's signing keys, which says nothing about the
	// token being checked.
	errSigningKeysUnavailable = errors.New("signing keys unavailable")
	// errApplicationToken is returned for tokens Auth0 issued to an application, which are not a user's.
	errApplicationToken = errors.New("token was issued to an application, not a user")
)

// auth0Claims are the claims Auth0 adds to access tokens that the validator checks.
type auth0Claims struct {
	// GrantType is how the token was obtained; Auth0 sets it to "client-credentials" for application tokens.
	GrantType string `json:"gty"`
}

// Validate rejects tokens issued through the client credentials grant, which identify an application.
func (c *auth0Claims) Validate(context.Context) error {
	if c.GrantType == auth0ClientCredentialsGrant {
		return errApplicationToken
	}
	return nil
}

// NewAuth0Validator returns a validator for "Bearer <jwt>" credentials: RS256 access tokens issued by
// issuerURL for audience to a user, with an expiry. The signing keys are discovered from issuerURL's OpenID
// configuration on first use and cached. The authenticated user ID is the token's subject.
//
// Tokens Auth0 issues to applications through the client credentials grant are rejected, so that an
// application authorised for audience is never treated as a user.
func NewAuth0Validator(issuerURL *url.URL, audience string) (AuthValidator, error) {
	provider, err := jwks.NewProvider(
		jwks.WithIssuerURL(issuerURL),
		jwks.WithCustomClient(&http.Client{Timeout: auth0KeysFetchTimeout}),
	)
	if err != nil {
		return nil, fmt.Errorf("creating signing key provider: %w", err)
	}
	keys := newAuth0KeyCache(provider.KeyFunc, time.Now)

	jwtValidator, err := validator.New(
		validator.WithKeyFunc(keys.get),
		validator.WithAlgorithm(validator.RS256),
		validator.WithIssuer(issuerURL.String()),
		validator.WithAudience(audience),
		validator.WithAllowedClockSkew(auth0ClockSkew),
		validator.WithRegisteredClaimsValidator(checkUserTokenClaims),
		validator.WithCustomClaims(func() *auth0Claims { return &auth0Claims{} }),
	)
	if err != nil {
		return nil, fmt.Errorf("creating JWT validator: %w", err)
	}

	return func(r *http.Request) (*Identity, error) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), auth0HeaderPrefix)
		if !ok {
			return nil, nil
		}
		if len(token) > maxAuth0TokenBytes {
			return nil, fmt.Errorf("%w: Auth0 token is longer than %d bytes", ErrInvalidCredentials, maxAuth0TokenBytes)
		}
		if err := checkAuth0TokenHeader(token); err != nil {
			return nil, fmt.Errorf("%w: Auth0 token: %w", ErrInvalidCredentials, err)
		}

		validated, err := jwtValidator.ValidateToken(r.Context(), token)
		if errors.Is(err, errSigningKeysUnavailable) {
			return nil, fmt.Errorf("validating Auth0 token: %w", err)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: Auth0 token: %w", ErrInvalidCredentials, err)
		}

		claims, ok := validated.(*validator.ValidatedClaims)
		if !ok {
			return nil, fmt.Errorf("%w: Auth0 token claims have unexpected type %T", ErrInvalidCredentials, validated)
		}

		return &Identity{UserID: claims.RegisteredClaims.Subject}, nil
	}, nil
}

// auth0TokenHeader is the part of a token's JOSE header that the validator checks itself, because the JWT
// library accepts a token with no key ID when the issuer publishes a single key, and ignores critical header
// extensions it does not implement.
type auth0TokenHeader struct {
	KeyID    string   `json:"kid"`
	Critical []string `json:"crit"`
}

// checkAuth0TokenHeader rejects tokens that do not name their signing key, or that list critical header
// extensions, none of which this validator implements. Only the header segment is decoded, so the work is
// bounded by the token's length, which the caller has already capped.
func checkAuth0TokenHeader(token string) error {
	encoded, _, _ := strings.Cut(token, ".")
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decoding token header: %w", err)
	}
	var header auth0TokenHeader
	if err := json.Unmarshal(decoded, &header); err != nil {
		return fmt.Errorf("parsing token header: %w", err)
	}
	switch {
	case header.KeyID == "":
		return errors.New("token header has no key ID")
	case header.Critical != nil:
		return errors.New("token header has critical extensions")
	}
	return nil
}

// checkUserTokenClaims checks what the JWT validator leaves optional: a token must expire, and must name a user
// as its subject.
func checkUserTokenClaims(claims validator.RegisteredClaims) error {
	switch {
	case claims.Expiry == 0:
		return errors.New("token has no expiry")
	case claims.Subject == "":
		return errors.New("token has no subject")
	case strings.HasSuffix(claims.Subject, auth0ClientSubjectSuffix):
		return errApplicationToken
	}
	return nil
}

// auth0KeyCache holds the issuer's signing keys for auth0KeysCacheTTL after each fetch, whatever caching the
// issuer's responses allow. Once the keys are older than that they are still returned while one background
// fetch replaces them; if that fetch fails they are dropped, so that the next request fetches them again.
//
// At most one fetch is in flight at a time. With no keys cached, every request waits for that fetch and shares
// its result, success or failure, but stops waiting when its own context ends. The fetch itself outlives the
// request that starts it, so a client that disconnects cannot make it fail; the fetch's HTTP client bounds it.
type auth0KeyCache struct {
	fetch func(context.Context) (any, error)
	now   func() time.Time

	// mu guards the fields below. It is never held during a fetch.
	mu        sync.Mutex
	keys      any
	expiresAt time.Time
	// inFlight is the fetch in progress, or nil if there is none.
	inFlight *auth0KeyFetch
}

// auth0KeyFetch is the result of one fetch of the signing keys, readable once done is closed.
type auth0KeyFetch struct {
	done chan struct{}
	keys any
	err  error
}

// newAuth0KeyCache returns a cache of the keys fetch returns, holding none yet.
func newAuth0KeyCache(fetch func(context.Context) (any, error), now func() time.Time) *auth0KeyCache {
	return &auth0KeyCache{fetch: fetch, now: now}
}

// get returns the cached keys, waiting for them to be fetched if none are cached. A failure to fetch them, or
// ctx ending first, wraps errSigningKeysUnavailable.
func (c *auth0KeyCache) get(ctx context.Context) (any, error) {
	c.mu.Lock()
	keys, inFlight := c.keys, c.inFlight
	if inFlight == nil && (keys == nil || !c.now().Before(c.expiresAt)) {
		inFlight = &auth0KeyFetch{done: make(chan struct{})}
		c.inFlight = inFlight
		go c.run(context.WithoutCancel(ctx), inFlight)
	}
	c.mu.Unlock()

	if keys != nil {
		return keys, nil
	}
	select {
	case <-inFlight.done:
		if inFlight.err != nil {
			return nil, fmt.Errorf("%w: %w", errSigningKeysUnavailable, inFlight.err)
		}
		return inFlight.keys, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("%w: waiting for fetch: %w", errSigningKeysUnavailable, context.Cause(ctx))
	}
}

// run makes the fetch f, then caches the keys it returns, or drops the cached keys if it fails.
func (c *auth0KeyCache) run(ctx context.Context, f *auth0KeyFetch) {
	f.keys, f.err = c.fetch(ctx)
	defer close(f.done)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.inFlight = nil
	if f.err != nil {
		c.keys = nil
		return
	}
	c.keys, c.expiresAt = f.keys, c.now().Add(auth0KeysCacheTTL)
}
