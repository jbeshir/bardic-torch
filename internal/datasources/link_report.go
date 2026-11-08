package datasources

import (
	"context"
	"errors"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The errors the link report writes refuse with.
var (
	// ErrReportLimitReached is a new report of a user with domain.MaxOpenLinkReports open reports.
	ErrReportLimitReached = errors.New("open link report limit reached")
	// ErrReportNotOpen is the resolution of a report that is already resolved or dismissed.
	ErrReportNotOpen = errors.New("the link report is not open")
)

// NewLinkReport is a user's report of the visible link whose URL normalizes to URL's.
type NewLinkReport struct {
	ReporterUserID string
	URL            string
	Reason         domain.LinkReportReason
	Message        *string
	Now            time.Time
}

// LinkReporter stores a user's report of a visible link: one that is not hidden and is attached to a public
// entity. It returns the reporter's open report of the link, if they have one, with created false, and otherwise
// the new report with created true. It returns ErrNotFound if no visible link has the URL, which is the same for a
// hidden link as for an unknown one, and ErrReportLimitReached if the reporter has domain.MaxOpenLinkReports open
// reports.
type LinkReporter interface {
	InsertLinkReport(ctx context.Context, report NewLinkReport) (receipt domain.LinkReportReceipt, created bool,
		err error)
}

// LinkReportQuery filters the link reports moderators list.
type LinkReportQuery struct {
	Status *domain.LinkReportStatus
	Page   domain.Page
}

// LinkReportLister lists one page of link reports for moderators, oldest first, then by ID.
type LinkReportLister interface {
	ListLinkReports(ctx context.Context, query LinkReportQuery) (domain.LinkReportList, error)
}

// ResolveLinkReportRequest is a moderator's resolution of the open link report ReportID: resolved or dismissed,
// with an optional note. HideLink also hides the report's link, as LinkHiddenSetter does.
type ResolveLinkReportRequest struct {
	ModeratorUserID string
	ReportID        int64
	Status          domain.LinkReportStatus
	Note            *string
	HideLink        bool
	Now             time.Time
}

// LinkReportResolver resolves or dismisses an open link report and returns it as it is then. It returns ErrNotFound
// for no such report and ErrReportNotOpen for a report that is not open.
type LinkReportResolver interface {
	ResolveLinkReport(ctx context.Context, req ResolveLinkReportRequest) (domain.LinkReport, error)
}

// SetLinkHiddenRequest is a moderator's takedown of the link LinkID, or its reversal.
type SetLinkHiddenRequest struct {
	ModeratorUserID string
	LinkID          int64
	Hidden          bool
	Now             time.Time
}

// LinkHiddenSetter hides or shows a link and returns it as it is then. Hiding a link resolves its open reports with
// the note domain.LinkHiddenNote. It returns ErrNotFound for no such link.
type LinkHiddenSetter interface {
	SetLinkHidden(ctx context.Context, req SetLinkHiddenRequest) (domain.ModerationLink, error)
}
