// Package turnstile verifies Cloudflare Turnstile tokens with Cloudflare's siteverify API.
package turnstile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// siteverifyURL is Cloudflare's server-side verification endpoint.
const siteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

// verifyTimeout bounds one verification, from sending the request to reading the answer.
const verifyTimeout = 5 * time.Second

// maxResponseBytes is the largest siteverify answer read; a larger one is a failure on Cloudflare's side.
const maxResponseBytes = 64 << 10

// userErrorCodes are the siteverify error codes the token, and so its sender, caused. Every other code is a
// misconfiguration or a failure on Cloudflare's side.
var userErrorCodes = []string{"missing-input-response", "invalid-input-response", "timeout-or-duplicate"}

var _ datasources.TurnstileVerifier = (*Verifier)(nil)

// Verifier verifies Turnstile tokens with a site's secret key, running at most
// domain.MaxConcurrentTurnstileVerifications verifications at once. The slots bound the outbound calls only: a slot
// is released when its verification returns, before the contact request is stored, so they do not bound how far
// concurrent requests can pass the open-request cap, whose count is not locked (a soft backstop until BE5's rate
// limits).
type Verifier struct {
	secret    string
	hostnames []string
	client    *http.Client
	endpoint  string
	slots     chan struct{}
}

// New returns a Verifier that verifies tokens with secret through client. A token passes only if Cloudflare
// accepts it, it was minted for one of hostnames (any hostname, if hostnames is empty) and for the action
// domain.TurnstileActionContact.
func New(secret string, hostnames []string, client *http.Client) *Verifier {
	return &Verifier{
		secret:    secret,
		hostnames: hostnames,
		client:    client,
		endpoint:  siteverifyURL,
		slots:     make(chan struct{}, domain.MaxConcurrentTurnstileVerifications),
	}
}

// siteverifyAnswer is the part of a siteverify answer the checks read.
type siteverifyAnswer struct {
	Success    bool     `json:"success"`
	Hostname   string   `json:"hostname"`
	Action     string   `json:"action"`
	ErrorCodes []string `json:"error-codes"`
}

// VerifyTurnstile verifies token with siteverify. It returns nil if the token passes, an error wrapping
// datasources.ErrTurnstileRejected if the token's sender is at fault, and one wrapping
// datasources.ErrTurnstileUnavailable otherwise: when Cloudflare cannot be reached, answers late or unreadably,
// reports a misconfiguration or its own failure, or when every verification slot is taken. Failures on
// Cloudflare's side and the slots being full are logged at warn, misconfigurations at error; the token is never
// logged.
func (v *Verifier) VerifyTurnstile(ctx context.Context, token string) error {
	logger := domain.LoggerFromContext(ctx)
	select {
	case v.slots <- struct{}{}:
		defer func() { <-v.slots }()
	default:
		logger.WarnContext(ctx, "turnstile verification refused: every verification slot is taken")
		return fmt.Errorf("%w: every verification slot is taken", datasources.ErrTurnstileUnavailable)
	}

	answer, err := v.siteverify(ctx, token)
	if err != nil {
		logger.WarnContext(ctx, "turnstile verification failed", "error", err)
		return fmt.Errorf("%w: %w", datasources.ErrTurnstileUnavailable, err)
	}

	switch {
	case !answer.Success && len(answer.ErrorCodes) > 0 && allUserErrors(answer.ErrorCodes):
		logger.InfoContext(ctx, "turnstile token rejected", "error_codes", answer.ErrorCodes)
		return fmt.Errorf("%w: %s", datasources.ErrTurnstileRejected, strings.Join(answer.ErrorCodes, ", "))
	case !answer.Success:
		logger.ErrorContext(ctx, "turnstile verification failed on Cloudflare's side or is misconfigured",
			"error_codes", answer.ErrorCodes)
		return fmt.Errorf("%w: error codes [%s]", datasources.ErrTurnstileUnavailable,
			strings.Join(answer.ErrorCodes, ", "))
	case len(v.hostnames) > 0 && !slices.Contains(v.hostnames, answer.Hostname),
		answer.Action != domain.TurnstileActionContact:
		logger.WarnContext(ctx, "turnstile token minted for another hostname or action",
			"hostname", answer.Hostname, "action", answer.Action)
		return fmt.Errorf("%w: minted for another hostname or action", datasources.ErrTurnstileRejected)
	}

	return nil
}

// siteverify posts token and the secret to siteverify and returns its answer, failing if the call does not
// finish within verifyTimeout or the answer is not a 200 with a JSON body of at most maxResponseBytes. Its errors
// never hold the token or the secret.
func (v *Verifier) siteverify(ctx context.Context, token string) (siteverifyAnswer, error) {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	form := url.Values{"secret": {v.secret}, "response": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return siteverifyAnswer{}, fmt.Errorf("creating siteverify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := v.client.Do(req)
	if err != nil {
		// A url.Error names the URL, which holds neither the token nor the secret: they are in the body.
		return siteverifyAnswer{}, fmt.Errorf("calling siteverify: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return siteverifyAnswer{}, fmt.Errorf("siteverify answered status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return siteverifyAnswer{}, fmt.Errorf("reading siteverify answer: %w", err)
	}
	if len(body) > maxResponseBytes {
		return siteverifyAnswer{}, errors.New("siteverify answer is too large")
	}
	var answer siteverifyAnswer
	if err := json.Unmarshal(body, &answer); err != nil {
		return siteverifyAnswer{}, fmt.Errorf("decoding siteverify answer: %w", err)
	}

	return answer, nil
}

// allUserErrors reports whether every one of codes is one the token's sender caused.
func allUserErrors(codes []string) bool {
	for _, code := range codes {
		if !slices.Contains(userErrorCodes, code) {
			return false
		}
	}

	return true
}
