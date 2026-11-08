package app

import (
	"context"
	"fmt"
	"net/netip"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/ratelimit"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
)

// Environment variables that configure how the API derives a client's IP (docs/PLAN.md §5.1 "Rate limits").
const (
	trustedProxyCIDRsEnv = "TRUSTED_PROXY_CIDRS"
	feProxySecretEnv     = "FE_PROXY_SECRET"
)

// rateLimits returns a limiter for each of tiers, timed by now.
func rateLimits(tiers ratelimit.Tiers, now func() time.Time) router.RateLimits {
	return router.RateLimits{
		Contact:             ratelimit.New(tiers.Contact, now),
		ContactUnattributed: ratelimit.New(tiers.ContactUnattributed, now),
		LinkReport:          ratelimit.New(tiers.LinkReport, now),
		DraftWrite:          ratelimit.New(tiers.DraftWrite, now),
		RevisionWrite:       ratelimit.New(tiers.RevisionWrite, now),
		RepertoireWrite:     ratelimit.New(tiers.RepertoireWrite, now),
		ProfileWrite:        ratelimit.New(tiers.ProfileWrite, now),
		MCP:                 ratelimit.New(tiers.MCP, now),
	}
}

// clientIPConfigFromEnv returns how the API derives a client's IP: TRUSTED_PROXY_CIDRS, comma-separated networks
// in CIDR notation, are the reverse proxies whose X-Forwarded-For is believed, and FE_PROXY_SECRET is the secret
// the FE attributes requests with. Both must be set. An empty FE_PROXY_SECRET attributes no request, and is logged
// at warn, as then every contact form message the FE relays counts against one limit for the FE's IPs; so is an
// empty TRUSTED_PROXY_CIDRS, as behind a proxy every request would then be from the proxy's IP. The secret is
// never logged.
func clientIPConfigFromEnv(ctx context.Context) (router.ClientIPConfig, error) {
	var proxies []netip.Prefix
	for _, cidr := range MustGetEnvAsStrings(ctx, trustedProxyCIDRsEnv) {
		network, err := netip.ParsePrefix(cidr)
		if err != nil {
			return router.ClientIPConfig{}, fmt.Errorf("parsing %s: %w", trustedProxyCIDRsEnv, err)
		}
		proxies = append(proxies, network.Masked())
	}
	secret := MustGetEnvAsString(ctx, feProxySecretEnv)

	logger := domain.LoggerFromContext(ctx)
	if len(proxies) == 0 {
		logger.WarnContext(ctx, "no reverse proxy is trusted to report client IPs: TRUSTED_PROXY_CIDRS is empty")
	}
	if secret == "" {
		logger.WarnContext(ctx, "the FE cannot attribute contact requests to client IPs: FE_PROXY_SECRET is empty")
	}

	return router.ClientIPConfig{TrustedProxies: proxies, ProxySecret: secret}, nil
}
