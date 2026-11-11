package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/turnstile"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// turnstileAcceptAll is the verifier when TURNSTILE_SECRET_KEY is empty, for development: it passes every token.
type turnstileAcceptAll struct{}

func (turnstileAcceptAll) VerifyTurnstile(context.Context, string) (bool, error) { return true, nil }

// turnstileFromEnv returns the contact form's verifier: Cloudflare's, with TURNSTILE_SECRET_KEY and the
// TURNSTILE_HOSTNAME tokens must be minted on, or, with an empty secret, one that passes every token (logged at
// warn). Both variables must be set, and the hostname must not be empty with a secret.
func turnstileFromEnv(ctx context.Context) (datasources.TurnstileVerifier, error) {
	secret := MustGetEnvAsString(ctx, "TURNSTILE_SECRET_KEY")
	hostname := MustGetEnvAsString(ctx, "TURNSTILE_HOSTNAME")
	if secret == "" {
		domain.LoggerFromContext(ctx).WarnContext(ctx,
			"TURNSTILE_SECRET_KEY is empty: the contact form accepts every Turnstile token")
		return turnstileAcceptAll{}, nil
	}
	if hostname == "" {
		return nil, errors.New("TURNSTILE_HOSTNAME is empty with TURNSTILE_SECRET_KEY set")
	}

	return &turnstile.Client{Secret: secret, Hostname: hostname, Endpoint: turnstile.SiteverifyURL,
		HTTP: &http.Client{}}, nil
}
