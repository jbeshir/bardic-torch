package datasources

import (
	"context"
	"errors"
)

// The errors a Turnstile verification fails with, by whose fault it is.
var (
	// ErrTurnstileRejected is a token that does not pass: Cloudflare refused it as missing, invalid, expired or
	// already used, or it was minted for another hostname or action. The sender is at fault.
	ErrTurnstileRejected = errors.New("turnstile token rejected")
	// ErrTurnstileUnavailable is a verification that could not be made: Cloudflare failed or could not be
	// reached in time, the secret is misconfigured, or too many verifications are running. The sender is not at
	// fault, and trying again later may work.
	ErrTurnstileUnavailable = errors.New("turnstile verification unavailable")
)

// TurnstileVerifier checks a Cloudflare Turnstile token server-side (D21). It returns nil if the token passes,
// and otherwise an error wrapping ErrTurnstileRejected or ErrTurnstileUnavailable. The token is a single-use
// secret: it is never logged or named in an error.
type TurnstileVerifier interface {
	VerifyTurnstile(ctx context.Context, token string) error
}
