package app

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAudience    = "https://api.bardictorch.org"
	testAuth0Domain = "bardic-torch.eu.auth0.com"
)

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

func TestAuthValidatorsFromEnv_NoDriversIsAnonymous(t *testing.T) {
	setAuthEnv(t, map[string]string{authDriversEnv: " , "})

	validators, err := authValidatorsFromEnv(t.Context())

	require.NoError(t, err)
	assert.Empty(t, validators)
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
			env:     map[string]string{authDriversEnv: "password"},
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

			_, err := authValidatorsFromEnv(t.Context())

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
				_, _ = authValidatorsFromEnv(t.Context())
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
