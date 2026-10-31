package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/auth0/go-jwt-middleware/v2/jwks"
	"github.com/auth0/go-jwt-middleware/v2/validator"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

const (
	auth0HeaderPrefix = "Bearer auth0|"
	// maxAuth0TokenBytes bounds the tokens that are parsed at all. Auth0 access tokens are well under 2 KiB; the
	// JWT library splits its input on "." before checking the token's shape, so an unbounded token costs memory
	// in proportion to its length before it is rejected.
	maxAuth0TokenBytes = 8 << 10
	// auth0KeysCacheTTL is how long the issuer's signing keys are cached before they are fetched again.
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

// NewAuth0Validator returns a validator for "Bearer auth0|<jwt>" credentials: RS256 access tokens issued by
// issuerURL for audience to a user, with an expiry. The signing keys are discovered from issuerURL's OpenID
// configuration on first use and cached. The authenticated user ID is the token's subject.
//
// Tokens Auth0 issues to applications through the client credentials grant are rejected, so that an
// application authorised for audience is never treated as a user.
func NewAuth0Validator(issuerURL *url.URL, audience string) (AuthValidator, error) {
	provider := jwks.NewCachingProvider(issuerURL, auth0KeysCacheTTL,
		jwks.WithCustomClient(&http.Client{Timeout: auth0KeysFetchTimeout}))
	keyFunc := func(ctx context.Context) (any, error) {
		keys, err := provider.KeyFunc(ctx)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", errSigningKeysUnavailable, err)
		}
		return keys, nil
	}

	jwtValidator, err := validator.New(
		keyFunc,
		validator.RS256,
		issuerURL.String(),
		[]string{audience},
		validator.WithAllowedClockSkew(auth0ClockSkew),
		validator.WithCustomClaims(func() validator.CustomClaims { return &auth0Claims{} }),
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
		if err := checkUserTokenClaims(claims.RegisteredClaims); err != nil {
			return nil, fmt.Errorf("%w: Auth0 token: %w", ErrInvalidCredentials, err)
		}

		return &Identity{UserID: claims.RegisteredClaims.Subject, Method: domain.AuthMethodAuth0}, nil
	}, nil
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
