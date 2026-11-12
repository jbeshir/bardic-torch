package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// ErrTurnstileFailed is returned for a contact request whose Turnstile token does not pass.
var ErrTurnstileFailed = errors.New("the Turnstile check failed")

var _ Command[CreateContactRequestRequest, int64] = (*CreateContactRequest)(nil)

// CreateContactRequestRequest is the request for the CreateContactRequest command. Name, Email, Message and URL
// are personal data and are never logged.
type CreateContactRequestRequest struct {
	Kind    domain.ContactKind
	Name    *string
	Email   string
	Message string
	URL     *string
	Token   string
}

// CreateContactRequest stores a message from the contact form.
type CreateContactRequest struct {
	Verifier datasources.TurnstileVerifier
	Contacts datasources.Moderation
	Now      func() time.Time
}

// Execute checks the Turnstile token and stores the request, returning its ID. A token that does not pass
// returns ErrTurnstileFailed, and nothing is stored.
func (c *CreateContactRequest) Execute(ctx context.Context, req CreateContactRequestRequest) (int64, error) {
	ok, err := c.Verifier.VerifyTurnstile(ctx, req.Token)
	if err != nil {
		return 0, fmt.Errorf("verifying Turnstile token: %w", err)
	}
	if !ok {
		return 0, ErrTurnstileFailed
	}

	return c.Contacts.InsertContactRequest(ctx, datasources.NewContactRequest{
		Kind: req.Kind, Name: req.Name, Email: req.Email, Message: req.Message, URL: req.URL, Now: c.Now(),
	})
}
