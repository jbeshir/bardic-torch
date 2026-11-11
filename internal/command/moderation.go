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

var (
	_ Command[CreateLinkReportRequest, int64]     = (*CreateLinkReport)(nil)
	_ Command[CreateContactRequestRequest, int64] = (*CreateContactRequest)(nil)
)

// CreateLinkReportRequest is the request for the CreateLinkReport command.
type CreateLinkReportRequest struct {
	UserID  string
	URL     string
	Reason  domain.LinkReportReason
	Message *string
}

// CreateLinkReport stores a user's report of a visible link.
type CreateLinkReport struct {
	Reports datasources.Moderation
	Now     func() time.Time
}

// Execute normalizes the URL and stores the report, returning its ID. A URL that is not valid, or that no visible
// link has, returns an error wrapping datasources.ErrNotFound, and an open report of the link by the same user one
// wrapping datasources.ErrAlreadyReported.
func (c *CreateLinkReport) Execute(ctx context.Context, req CreateLinkReportRequest) (int64, error) {
	normalized, err := domain.NormalizeURL(req.URL)
	if err != nil {
		return 0, fmt.Errorf("link: %w", datasources.ErrNotFound)
	}

	return c.Reports.InsertLinkReport(ctx, datasources.NewLinkReport{
		ReporterUserID: req.UserID, NormalizedURL: normalized, Reason: req.Reason, Message: req.Message, Now: c.Now(),
	})
}

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
