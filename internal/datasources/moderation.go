package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// ErrNotOpen is the resolution of a contact request that is already closed.
var ErrNotOpen = errors.New("not open")

// NewContactRequest is a message sent through the contact form. Name, Email, Message and URL are personal data,
// which must never be logged.
type NewContactRequest struct {
	Kind    domain.ContactKind
	Name    *string
	Email   string
	Message string
	URL     *string
	Now     time.Time
}

// Closing is a moderator closing an open contact request.
type Closing struct {
	ModeratorUserID string
	ID              int64
	Status          domain.ReportStatus
	Note            *string
	Now             time.Time
}

// Moderation stores link takedowns and contact requests.
type Moderation interface {
	// GetModerationLink returns the link with the normalized URL, hidden or not, or ErrNotFound.
	GetModerationLink(ctx context.Context, normalizedURL string) (domain.ModerationLink, error)
	// SetLinkHidden hides or restores a link, or returns ErrNotFound.
	SetLinkHidden(ctx context.Context, linkID int64, hidden bool) error

	// InsertContactRequest stores a request and returns its ID.
	InsertContactRequest(ctx context.Context, request NewContactRequest) (int64, error)
	// ListContactRequests lists one page of the requests with the status, oldest first.
	ListContactRequests(
		ctx context.Context, status domain.ReportStatus, page domain.Page,
	) (domain.ContactRequestList, error)
	// CloseContactRequest resolves or dismisses an open request. It returns ErrNotFound for no such
	// request and ErrNotOpen for one already closed.
	CloseContactRequest(ctx context.Context, closing Closing) error
}

// TurnstileVerifier checks a Turnstile token from the contact form.
type TurnstileVerifier interface {
	// VerifyTurnstile reports whether the token passes. An error means the check could not be made.
	VerifyTurnstile(ctx context.Context, token string) (bool, error)
}
