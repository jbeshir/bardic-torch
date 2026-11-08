package app

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/turnstile"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// Environment variables that configure the contact form's Turnstile verification (D21).
const (
	turnstileSecretKeyEnv = "TURNSTILE_SECRET_KEY"
	turnstileHostnamesEnv = "TURNSTILE_HOSTNAMES"
)

// contactDisabled is the Turnstile verifier when TURNSTILE_SECRET_KEY is empty: no token can be verified, so the
// contact form answers 503 unavailable.
type contactDisabled struct{}

func (contactDisabled) VerifyTurnstile(context.Context, string) error {
	return fmt.Errorf("%w: the contact form is disabled, as TURNSTILE_SECRET_KEY is empty",
		datasources.ErrTurnstileUnavailable)
}

// contactVerifierFromEnv returns the contact form's Turnstile verifier: one verifying tokens with
// TURNSTILE_SECRET_KEY through client, accepting those minted on a hostname in TURNSTILE_HOSTNAMES, compared
// without regard to case, or contactDisabled, logged at warn, if the secret is empty. An empty TURNSTILE_HOSTNAMES
// accepts tokens minted on any hostname, which only development should do, so it is logged at warn when the secret
// is set. Both variables must be set.
func contactVerifierFromEnv(ctx context.Context, client *http.Client) datasources.TurnstileVerifier {
	secret := MustGetEnvAsString(ctx, turnstileSecretKeyEnv)
	hostnames := MustGetEnvAsStrings(ctx, turnstileHostnamesEnv)
	logger := domain.LoggerFromContext(ctx)
	if secret == "" {
		logger.WarnContext(ctx, "the contact form is disabled: TURNSTILE_SECRET_KEY is empty")
		return contactDisabled{}
	}
	if len(hostnames) == 0 {
		logger.WarnContext(ctx, "the contact form accepts Turnstile tokens minted on any hostname: "+
			"TURNSTILE_HOSTNAMES is empty, which only development should do")
	}

	return turnstile.New(secret, hostnames, client)
}
