package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	// ErrAlreadyReported is a link report by a user who already has an open report of the link.
	ErrAlreadyReported = errors.New("the user already has an open report of the link")
	// ErrNotOpen is the resolution of a link report or contact request that is already closed.
	ErrNotOpen = errors.New("not open")
)

// NewLinkReport is a user's report of the visible link whose normalized URL is NormalizedURL.
type NewLinkReport struct {
	ReporterUserID string
	NormalizedURL  string
	Reason         domain.LinkReportReason
	Message        *string
	Now            time.Time
}

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

// Closing is a moderator closing an open report or contact request.
type Closing struct {
	ModeratorUserID string
	ID              int64
	Status          domain.ReportStatus
	Note            *string
	Now             time.Time
}

// Moderation stores link reports, link takedowns and contact requests.
type Moderation interface {
	// InsertLinkReport stores a report and returns its ID. It returns ErrNotFound if no visible link (not hidden,
	// attached to a public entity) has the URL, and ErrAlreadyReported if the reporter has an open report of it.
	InsertLinkReport(ctx context.Context, report NewLinkReport) (int64, error)
	// ListLinkReports lists one page of the reports with the status, oldest first.
	ListLinkReports(ctx context.Context, status domain.ReportStatus, page domain.Page) (domain.LinkReportList, error)
	// CloseLinkReport resolves or dismisses an open report, and with hideLink also hides its link. It returns
	// ErrNotFound for no such report and ErrNotOpen for one already closed.
	CloseLinkReport(ctx context.Context, closing Closing, hideLink bool) error
	// GetModerationLink returns the link with the normalized URL, hidden or not, or ErrNotFound.
	GetModerationLink(ctx context.Context, normalizedURL string) (domain.ModerationLink, error)
	// SetLinkHidden hides or restores a link, or returns ErrNotFound.
	SetLinkHidden(ctx context.Context, linkID int64, hidden bool) error

	// InsertContactRequest stores a request and returns its ID.
	InsertContactRequest(ctx context.Context, request NewContactRequest) (int64, error)
	// ListContactRequests purges the requests due for it as of now, then lists one page of those with the status,
	// oldest first.
	ListContactRequests(
		ctx context.Context, status domain.ReportStatus, page domain.Page, now time.Time,
	) (domain.ContactRequestList, error)
	// CloseContactRequest resolves or dismisses an open request, as CloseLinkReport does a report.
	CloseContactRequest(ctx context.Context, closing Closing) error
}

// TurnstileVerifier checks a Turnstile token from the contact form.
type TurnstileVerifier interface {
	// VerifyTurnstile reports whether the token passes. An error means the check could not be made.
	VerifyTurnstile(ctx context.Context, token string) (bool, error)
}
