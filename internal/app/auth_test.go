package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testAudience    = "https://api.bardictorch.org"
	testAuth0Domain = "bardic-torch.eu.auth0.com"
)

type apiTokenAuthMocks struct {
	*mocks.APITokenByHashGetter
	*mocks.APITokenLastUsedUpdater
}

func newAPITokenAuthMocks(t *testing.T) apiTokenAuthMocks {
	return apiTokenAuthMocks{mocks.NewAPITokenByHashGetter(t), mocks.NewAPITokenLastUsedUpdater(t)}
}

// setAuthEnv sets the auth environment variables in env and unsets the others, for the rest of the test.
func setAuthEnv(t *testing.T, env map[string]string) {
	for _, name := range []string{authDriversEnv, auth0DomainEnv, auth0AudienceEnv} {
		t.Setenv(name, "")
		if value, ok := env[name]; ok {
			t.Setenv(name, value)
		} else {
			require.NoError(t, os.Unsetenv(name))
		}
	}
}

// authenticateWith runs authorization through each validator, returning the first applicable result.
func authenticateWith(t *testing.T, validators []router.AuthValidator, authorization string) (*router.Identity, error) {
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/tokens", nil)
	req.Header.Set("Authorization", authorization)
	for _, validate := range validators {
		if identity, err := validate(req); identity != nil || err != nil {
			return identity, err
		}
	}
	return nil, nil
}

func TestAuthValidatorsFromEnv_NoDriversIsAnonymous(t *testing.T) {
	setAuthEnv(t, map[string]string{authDriversEnv: " , "})

	validators, err := authValidatorsFromEnv(t.Context(), newAPITokenAuthMocks(t), time.Now)

	require.NoError(t, err)
	assert.Empty(t, validators)
}

func TestAuthValidatorsFromEnv_APITokenOnlyDoesNotReadAuth0(t *testing.T) {
	// AUTH0_DOMAIN and AUTH0_AUDIENCE are unset, so reading them would panic.
	setAuthEnv(t, map[string]string{authDriversEnv: apiTokenDriver})
	store := newAPITokenAuthMocks(t)
	store.APITokenByHashGetter.EXPECT().GetAPITokenByHash(mock.Anything, mock.Anything).
		Return(domain.APIToken{}, datasources.ErrNotFound)

	validators, err := authValidatorsFromEnv(t.Context(), store, time.Now)
	require.NoError(t, err)
	require.Len(t, validators, 1)

	_, err = authenticateWith(t, validators, "Bearer user_api|0123")
	require.ErrorIs(t, err, router.ErrInvalidCredentials, "API tokens are checked against the store")

	identity, err := authenticateWith(t, validators, "Bearer auth0|not-a-jwt")
	require.NoError(t, err)
	assert.Nil(t, identity, "Auth0 tokens are not accepted")
}

func TestAuthValidatorsFromEnv_BothDrivers(t *testing.T) {
	setAuthEnv(t, map[string]string{
		authDriversEnv:   "auth0, api_token",
		auth0DomainEnv:   testAuth0Domain,
		auth0AudienceEnv: testAudience,
	})

	validators, err := authValidatorsFromEnv(t.Context(), newAPITokenAuthMocks(t), time.Now)
	require.NoError(t, err)
	require.Len(t, validators, 2)

	// A malformed token is rejected before any signing keys are fetched.
	_, err = authenticateWith(t, validators, "Bearer auth0|not-a-jwt")
	require.ErrorIs(t, err, router.ErrInvalidCredentials)
}

func TestAuthValidatorsFromEnv_Auth0OnlyIgnoresAPITokens(t *testing.T) {
	setAuthEnv(t, map[string]string{
		authDriversEnv:   auth0Driver,
		auth0DomainEnv:   testAuth0Domain,
		auth0AudienceEnv: testAudience,
	})

	// The API token store mocks fail the test if they are used.
	validators, err := authValidatorsFromEnv(t.Context(), newAPITokenAuthMocks(t), time.Now)
	require.NoError(t, err)
	require.Len(t, validators, 1)

	identity, err := authenticateWith(t, validators, "Bearer user_api|0123")
	require.NoError(t, err)
	assert.Nil(t, identity, "API tokens are not accepted")

	_, err = authenticateWith(t, validators, "Bearer auth0|not-a-jwt")
	require.ErrorIs(t, err, router.ErrInvalidCredentials, "Auth0 tokens are checked")
}

func TestAuthValidatorsFromEnv_InvalidConfiguration(t *testing.T) {
	const emptyAuth0Config = "AUTH0_DOMAIN and AUTH0_AUDIENCE must not be empty when the auth0 driver is enabled"

	cases := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{
			name:    "unknown_driver",
			env:     map[string]string{authDriversEnv: "api_token,password"},
			wantErr: "unknown auth driver [password]",
		},
		{
			name:    "empty_auth0_domain",
			env:     map[string]string{authDriversEnv: auth0Driver, auth0DomainEnv: "", auth0AudienceEnv: testAudience},
			wantErr: emptyAuth0Config,
		},
		{
			name:    "empty_auth0_audience",
			env:     map[string]string{authDriversEnv: auth0Driver, auth0DomainEnv: testAuth0Domain, auth0AudienceEnv: ""},
			wantErr: emptyAuth0Config,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setAuthEnv(t, tc.env)

			_, err := authValidatorsFromEnv(t.Context(), newAPITokenAuthMocks(t), time.Now)

			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestAuthValidatorsFromEnv_MissingAuth0SettingPanics(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{name: "missing_domain", env: map[string]string{authDriversEnv: auth0Driver, auth0AudienceEnv: testAudience}},
		{name: "missing_audience", env: map[string]string{authDriversEnv: auth0Driver, auth0DomainEnv: testAuth0Domain}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setAuthEnv(t, tc.env)

			assert.Panics(t, func() {
				_, _ = authValidatorsFromEnv(t.Context(), newAPITokenAuthMocks(t), time.Now)
			})
		})
	}
}

func TestAuth0IssuerURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		domain string
		want   string
	}{
		{domain: testAuth0Domain, want: "https://bardic-torch.eu.auth0.com/"},
		{domain: "login.bardictorch.org", want: "https://login.bardictorch.org/"},
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, auth0IssuerURL(tc.domain).String(), tc.domain)
	}
}
