// Package turnstile verifies Cloudflare Turnstile tokens with Cloudflare's siteverify API.
package turnstile

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// SiteverifyURL is Cloudflare's server-side verification endpoint.
const SiteverifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

const (
	verifyTimeout    = 5 * time.Second
	maxResponseBytes = 64 << 10
)

var _ datasources.TurnstileVerifier = (*Client)(nil)

// Client verifies tokens with a site's secret key.
type Client struct {
	// Secret is the site's secret key.
	Secret string
	// Hostname is the hostname a token must have been minted on, compared without regard to case.
	Hostname string
	// Endpoint is the siteverify URL, SiteverifyURL in production.
	Endpoint string
	HTTP     *http.Client
}

// VerifyTurnstile reports whether Cloudflare accepts the token and it was minted on c.Hostname for the action
// domain.TurnstileActionContact. An error means Cloudflare could not be asked or answered unreadably. Errors
// never hold the token or the secret.
func (c *Client) VerifyTurnstile(ctx context.Context, token string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, verifyTimeout)
	defer cancel()

	form := url.Values{"secret": {c.Secret}, "response": {token}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return false, fmt.Errorf("creating siteverify request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, fmt.Errorf("calling siteverify: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("siteverify answered status %d", resp.StatusCode)
	}

	var answer struct {
		Success  bool   `json:"success"`
		Hostname string `json:"hostname"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&answer); err != nil {
		return false, fmt.Errorf("decoding siteverify answer: %w", err)
	}

	return answer.Success && strings.EqualFold(answer.Hostname, c.Hostname) &&
		answer.Action == domain.TurnstileActionContact, nil
}
