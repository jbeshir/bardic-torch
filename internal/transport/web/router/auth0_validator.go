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
	// auth0KeysCacheTTL is how long the issuer's signing keys are cached before they are fetched again.
	auth0KeysCacheTTL = 5 * time.Minute
	// auth0KeysFetchTimeout bounds each fetch of the issuer's discovery document and signing keys.
	auth0KeysFetchTimeout = 10 * time.Second
	// auth0ClockSkew is how far token time claims may be off from this server's clock.
	auth0ClockSkew = time.Minute
)

// errSigningKeysUnavailable marks a failure to fetch the issuer's signing keys, which says nothing about the
// token being checked.
var errSigningKeysUnavailable = errors.New("signing keys unavailable")

// NewAuth0Validator returns a validator for "Bearer auth0|<jwt>" credentials: RS256 access tokens issued by
// issuerURL for audience. The signing keys are discovered from issuerURL's OpenID configuration on first use and
// cached. The authenticated user ID is the token's subject.
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
	)
	if err != nil {
		return nil, fmt.Errorf("creating JWT validator: %w", err)
	}

	return func(r *http.Request) (*Identity, error) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), auth0HeaderPrefix)
		if !ok {
			return nil, nil
		}

		validated, err := jwtValidator.ValidateToken(r.Context(), token)
		if errors.Is(err, errSigningKeysUnavailable) {
			return nil, fmt.Errorf("validating Auth0 token: %w", err)
		}
		if err != nil {
			return nil, fmt.Errorf("%w: Auth0 token: %w", ErrInvalidCredentials, err)
		}

		claims, ok := validated.(*validator.ValidatedClaims)
		if !ok || claims.RegisteredClaims.Subject == "" {
			return nil, fmt.Errorf("%w: Auth0 token has no subject", ErrInvalidCredentials)
		}

		return &Identity{UserID: claims.RegisteredClaims.Subject, Method: domain.AuthMethodAuth0}, nil
	}, nil
}
