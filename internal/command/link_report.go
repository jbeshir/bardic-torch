package command

import (
	"context"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ Command[ReportLinkRequest, ReportedLink]              = (*ReportLink)(nil)
	_ Command[LinkReportListRequest, domain.LinkReportList] = (*ListLinkReports)(nil)
	_ Command[ResolveLinkReportRequest, domain.LinkReport]  = (*ResolveLinkReport)(nil)
	_ Command[SetLinkHiddenRequest, domain.ModerationLink]  = (*SetLinkHidden)(nil)
	_ Command[ModerationLinkRequest, domain.ModerationLink] = (*GetModerationLink)(nil)
)

// ReportLinkRequest is Principal's report of the link a public page shows with URL (D21).
type ReportLinkRequest struct {
	Principal domain.Principal
	URL       string
	Reason    domain.LinkReportReason
	Message   *string
}

// ReportedLink is the receipt of a link report; Created is unset when the reporter's open report of the link was
// returned instead of a new one.
type ReportedLink struct {
	Receipt domain.LinkReportReceipt
	Created bool
}

// ReportLink reports a visible link. Any user who is not blocked may, with either credential (D25).
type ReportLink struct {
	Reporter datasources.LinkReporter
	Now      func() time.Time
}

// Execute stores the principal's report, or returns their open report of the link. It fails with the errors of
// datasources.LinkReporter: datasources.ErrNotFound for a URL no visible link has, hidden or unknown alike.
func (c *ReportLink) Execute(ctx context.Context, req ReportLinkRequest) (ReportedLink, error) {
	receipt, created, err := c.Reporter.InsertLinkReport(ctx, datasources.NewLinkReport{
		ReporterUserID: req.Principal.User.ID, URL: req.URL, Reason: req.Reason, Message: req.Message, Now: c.Now(),
	})
	if err != nil {
		return ReportedLink{}, fmt.Errorf("reporting link: %w", err)
	}

	return ReportedLink{Receipt: receipt, Created: created}, nil
}

// LinkReportListRequest is Principal's request for a page of the link reports.
type LinkReportListRequest struct {
	Principal domain.Principal
	Query     datasources.LinkReportQuery
}

// ListLinkReports lists the link reports for moderators, oldest first.
type ListLinkReports struct {
	Lister datasources.LinkReportLister
}

// Execute returns the page of link reports req.Query asks for.
func (c *ListLinkReports) Execute(ctx context.Context, req LinkReportListRequest) (domain.LinkReportList, error) {
	if _, err := moderatorActor(req.Principal); err != nil {
		return domain.LinkReportList{}, err
	}
	list, err := c.Lister.ListLinkReports(ctx, req.Query)
	if err != nil {
		return domain.LinkReportList{}, fmt.Errorf("listing link reports: %w", err)
	}

	return list, nil
}

// ResolveLinkReportRequest is Principal's resolution of the open link report ReportID as Status, resolved or
// dismissed, with an optional note; HideLink also hides the report's link.
type ResolveLinkReportRequest struct {
	Principal domain.Principal
	ReportID  int64
	Status    domain.LinkReportStatus
	Note      *string
	HideLink  bool
}

// ResolveLinkReport resolves or dismisses an open link report.
type ResolveLinkReport struct {
	Resolver datasources.LinkReportResolver
	Now      func() time.Time
}

// Execute resolves the report and returns it, failing with the errors of datasources.LinkReportResolver.
func (c *ResolveLinkReport) Execute(ctx context.Context, req ResolveLinkReportRequest) (domain.LinkReport, error) {
	actor, err := moderatorActor(req.Principal)
	if err != nil {
		return domain.LinkReport{}, err
	}
	report, err := c.Resolver.ResolveLinkReport(ctx, datasources.ResolveLinkReportRequest{
		ModeratorUserID: actor.UserID, ReportID: req.ReportID, Status: req.Status, Note: req.Note,
		HideLink: req.HideLink, Now: c.Now(),
	})
	if err != nil {
		return domain.LinkReport{}, fmt.Errorf("resolving link report [%d]: %w", req.ReportID, err)
	}

	return report, nil
}

// SetLinkHiddenRequest is Principal's takedown of the link LinkID, or its reversal.
type SetLinkHiddenRequest struct {
	Principal domain.Principal
	LinkID    int64
	Hidden    bool
}

// SetLinkHidden hides or shows a link.
type SetLinkHidden struct {
	Setter datasources.LinkHiddenSetter
	Now    func() time.Time
}

// Execute sets the link's hidden state and returns it, failing with datasources.ErrNotFound for no such link.
func (c *SetLinkHidden) Execute(ctx context.Context, req SetLinkHiddenRequest) (domain.ModerationLink, error) {
	actor, err := moderatorActor(req.Principal)
	if err != nil {
		return domain.ModerationLink{}, err
	}
	link, err := c.Setter.SetLinkHidden(ctx, datasources.SetLinkHiddenRequest{
		ModeratorUserID: actor.UserID, LinkID: req.LinkID, Hidden: req.Hidden, Now: c.Now(),
	})
	if err != nil {
		return domain.ModerationLink{}, fmt.Errorf("setting link [%d] hidden: %w", req.LinkID, err)
	}

	return link, nil
}

// ModerationLinkRequest is Principal's lookup of the link with URL, hidden or not.
type ModerationLinkRequest struct {
	Principal domain.Principal
	URL       string
}

// GetModerationLink finds any link by URL for moderators.
type GetModerationLink struct {
	Getter datasources.ModerationLinkGetter
}

// Execute returns the link whose normalized URL is req.URL's, failing with datasources.ErrNotFound if there is
// none.
func (c *GetModerationLink) Execute(ctx context.Context, req ModerationLinkRequest) (domain.ModerationLink, error) {
	if _, err := moderatorActor(req.Principal); err != nil {
		return domain.ModerationLink{}, err
	}
	// The URL is not named: it may be a hidden link's.
	link, err := c.Getter.GetModerationLink(ctx, req.URL)
	if err != nil {
		return domain.ModerationLink{}, fmt.Errorf("getting moderation link: %w", err)
	}

	return link, nil
}
