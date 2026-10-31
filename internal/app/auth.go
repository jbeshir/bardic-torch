package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
)

// Environment variables and driver names that configure authentication.
const (
	authDriversEnv   = "AUTH_DRIVERS"
	auth0DomainEnv   = "AUTH0_DOMAIN"
	auth0AudienceEnv = "AUTH0_AUDIENCE"

	auth0Driver    = "auth0"
	apiTokenDriver = "api_token"
)

// apiTokenAuthStore is what the API token validator reads and writes.
type apiTokenAuthStore interface {
	datasources.APITokenByHashGetter
	datasources.APITokenLastUsedUpdater
}

// authValidatorsFromEnv returns a validator for each driver listed in AUTH_DRIVERS: "auth0" or "api_token".
// With no drivers every request is anonymous, so authenticated endpoints answer 401. AUTH0_DOMAIN and
// AUTH0_AUDIENCE are read only when auth0 is listed.
func authValidatorsFromEnv(
	ctx context.Context,
	apiTokens apiTokenAuthStore,
	now func() time.Time,
) ([]router.AuthValidator, error) {
	var validators []router.AuthValidator
	for _, driver := range MustGetEnvAsStrings(ctx, authDriversEnv) {
		switch driver {
		case auth0Driver:
			validator, err := auth0ValidatorFromEnv(ctx)
			if err != nil {
				return nil, err
			}
			validators = append(validators, validator)
		case apiTokenDriver:
			validators = append(validators, router.NewAPITokenValidator(apiTokens, apiTokens, now))
		default:
			return nil, fmt.Errorf("unknown auth driver [%s]", driver)
		}
	}

	return validators, nil
}

// auth0ValidatorFromEnv returns a validator for tokens that the Auth0 tenant at AUTH0_DOMAIN issued for
// AUTH0_AUDIENCE.
func auth0ValidatorFromEnv(ctx context.Context) (router.AuthValidator, error) {
	domain := MustGetEnvAsString(ctx, auth0DomainEnv)
	audience := MustGetEnvAsString(ctx, auth0AudienceEnv)
	if domain == "" || audience == "" {
		return nil, errors.New("AUTH0_DOMAIN and AUTH0_AUDIENCE must not be empty when the auth0 driver is enabled")
	}

	validator, err := router.NewAuth0Validator(auth0IssuerURL(domain), audience)
	if err != nil {
		return nil, fmt.Errorf("creating Auth0 validator: %w", err)
	}

	return validator, nil
}

// auth0IssuerURL returns the issuer of tokens from the Auth0 tenant at domain: its root URL, trailing slash
// included, which is also where its OpenID configuration is discovered from.
func auth0IssuerURL(domain string) *url.URL {
	return &url.URL{Scheme: "https", Host: domain, Path: "/"}
}
