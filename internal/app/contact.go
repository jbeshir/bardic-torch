package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/turnstile"
)

// turnstileFromEnv returns Cloudflare's verifier for the contact form, with TURNSTILE_SECRET_KEY and the
// TURNSTILE_HOSTNAME tokens must be minted on. Both must be set and non-empty.
func turnstileFromEnv(ctx context.Context) (datasources.TurnstileVerifier, error) {
	secret := MustGetEnvAsString(ctx, "TURNSTILE_SECRET_KEY")
	hostname := MustGetEnvAsString(ctx, "TURNSTILE_HOSTNAME")
	if secret == "" || hostname == "" {
		return nil, errors.New("TURNSTILE_SECRET_KEY and TURNSTILE_HOSTNAME must not be empty")
	}

	return &turnstile.Client{Secret: secret, Hostname: hostname, Endpoint: turnstile.SiteverifyURL,
		HTTP: &http.Client{}}, nil
}
