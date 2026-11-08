package app

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/ratelimit"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/mcp"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
	"github.com/jbeshir/bardic-torch/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// testMCPTools are the MCP tools of the embedded spec, as Setup builds them.
func testMCPTools(t *testing.T) *mcp.Toolset {
	t.Helper()

	tools, err := embeddedMCPTools()
	require.NoError(t, err)
	return tools
}

var embeddedMCPTools = sync.OnceValues(func() (*mcp.Toolset, error) { return mcp.NewToolset(openapi.Spec) })

// unlimitedRateLimits are rate limits no test reaches, for the tests of everything but rate limiting.
func unlimitedRateLimits() router.RateLimits {
	tier := ratelimit.Tier{Name: "unlimited", Burst: 1 << 30, Interval: time.Hour, MaxKeys: 1 << 16}
	return rateLimits(ratelimit.Tiers{
		Contact: tier, ContactUnattributed: tier, LinkReport: tier, DraftWrite: tier, RevisionWrite: tier,
		RepertoireWrite: tier, ProfileWrite: tier, MCP: tier,
	}, time.Now)
}

// TestRateLimits_OneLimiterPerTier checks that each route's limiter is built from its own tier.
func TestRateLimits_OneLimiterPerTier(t *testing.T) {
	t.Parallel()

	tiers := ratelimit.DefaultTiers()
	limits := reflect.ValueOf(rateLimits(tiers, time.Now))
	tierValues := reflect.ValueOf(tiers)
	require.Equal(t, tierValues.NumField(), limits.NumField(), "every tier needs a limiter")

	for i := range tierValues.NumField() {
		name := tierValues.Type().Field(i).Name
		tier, ok := tierValues.Field(i).Interface().(ratelimit.Tier)
		require.True(t, ok)
		limiter, ok := limits.FieldByName(name).Interface().(*ratelimit.Limiter)
		require.Truef(t, ok, "no limiter %s", name)
		assert.Equalf(t, tier.Name, limiter.Name(), "limiter %s", name)
	}
}

func TestClientIPConfigFromEnv(t *testing.T) {
	const secret = "a-proxy-secret-for-tests"

	for _, tc := range []struct {
		name       string
		cidrs      string
		secret     string
		want       router.ClientIPConfig
		wantErr    bool
		wantWarned []string
	}{
		{
			name: "production", cidrs: "172.16.0.0/12", secret: secret,
			want: router.ClientIPConfig{
				TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.16.0.0/12")}, ProxySecret: secret,
			},
		},
		{
			name: "development", cidrs: " 127.0.0.0/8 , ::1/128 ", secret: secret,
			want: router.ClientIPConfig{
				TrustedProxies: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")},
				ProxySecret:    secret,
			},
		},
		{
			name: "host bits are masked", cidrs: "172.17.0.1/16", secret: secret,
			want: router.ClientIPConfig{
				TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.17.0.0/16")}, ProxySecret: secret,
			},
		},
		{
			name: "empty", cidrs: "", secret: "", want: router.ClientIPConfig{},
			wantWarned: []string{"TRUSTED_PROXY_CIDRS is empty", "FE_PROXY_SECRET is empty"},
		},
		{name: "not a network", cidrs: "172.16.0.0", secret: secret, wantErr: true},
		{name: "garbage", cidrs: "127.0.0.0/8,proxy", secret: secret, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(trustedProxyCIDRsEnv, tc.cidrs)
			t.Setenv(feProxySecretEnv, tc.secret)

			var logs bytes.Buffer
			ctx := domain.ContextWithLogger(t.Context(), slog.New(slog.NewJSONHandler(&logs, nil)))
			got, err := clientIPConfigFromEnv(ctx)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			for _, warning := range tc.wantWarned {
				assert.Contains(t, logs.String(), warning)
			}
			if tc.wantWarned == nil {
				assert.Empty(t, logs.String())
			}
			assert.NotContains(t, logs.String(), secret, "the secret is logged")
		})
	}
}

// TestContactSubmit_RateLimitedBeforeTurnstile checks, through the production handler, that a contact form
// message over its client IP's limit is refused before Turnstile is asked or anything is stored.
func TestContactSubmit_RateLimitedBeforeTurnstile(t *testing.T) {
	t.Parallel()

	tier := ratelimit.Tier{Name: "contact", Burst: 1, Interval: time.Hour, MaxKeys: 10}
	verifier := mocks.NewTurnstileVerifier(t)
	verifier.EXPECT().VerifyTurnstile(mock.Anything, testTurnstileToken).Return(nil).Once()
	creator := mocks.NewContactRequestCreator(t)
	creator.EXPECT().CreateContactRequest(mock.Anything, mock.Anything).Return(7, nil).Once()
	handler := NewHTTPHandler(HTTPDependencies{
		ContactRequestCreator: creator,
		TurnstileVerifier:     verifier,
		RateLimits: rateLimits(ratelimit.Tiers{
			Contact: tier, ContactUnattributed: tier, LinkReport: tier, DraftWrite: tier, RevisionWrite: tier,
			RepertoireWrite: tier, ProfileWrite: tier, MCP: tier,
		}, func() time.Time { return handlerNow }),
		Now: func() time.Time { return handlerNow },
	})

	contract := openapitest.New(t)
	for _, want := range []int{http.StatusAccepted, http.StatusTooManyRequests} {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/contact", strings.NewReader(
			`{"kind":"general","email":"a@b","message":"Hello.","turnstile_token":"`+testTurnstileToken+`"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := contract.Serve(t, handler, req)
		require.Equal(t, want, rec.Code, rec.Body.String())
	}
}
