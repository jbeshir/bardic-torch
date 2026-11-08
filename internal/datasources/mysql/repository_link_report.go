package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.LinkReporter       = (*Repository)(nil)
	_ datasources.LinkReportLister   = (*Repository)(nil)
	_ datasources.LinkReportResolver = (*Repository)(nil)
	_ datasources.LinkHiddenSetter   = (*Repository)(nil)
)

// InsertLinkReport stores a user's report of a visible link. Under the reporter's users row lock, which
// serialises their reports, it locks the visible link by its normalized URL FOR SHARE, so that it cannot be hidden
// before the report commits, then returns the reporter's open report of it, or refuses a report of a link whose
// reporter's own report of it was dismissed less than domain.LinkReportCooldown ago, or counts their open reports
// and inserts. The URL is never named in errors: it may be a hidden link's.
func (r *Repository) InsertLinkReport(
	ctx context.Context, report datasources.NewLinkReport,
) (domain.LinkReportReceipt, bool, error) {
	normalized, err := domain.NormalizeURL(report.URL)
	if err != nil {
		return domain.LinkReportReceipt{}, false, fmt.Errorf("reporting link: %w", datasources.ErrNotFound)
	}
	var (
		receipt domain.LinkReportReceipt
		created bool
	)
	err = r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		if _, err := c.lockUser(ctx, report.ReporterUserID); err != nil {
			return err
		}
		linkID, err := c.q.LockVisibleLink(ctx, normalized)
		if err != nil {
			return lookupError("link", err)
		}
		existing, err := c.q.GetOpenLinkReportOfReporter(ctx, queries.GetOpenLinkReportOfReporterParams{
			ReporterUserID: report.ReporterUserID, LinkID: linkID,
		})
		switch {
		case err == nil:
			receipt, created = domain.LinkReportReceipt{ID: c.signedID(existing.ID),
				Status: enumOf[domain.LinkReportStatus](c, existing.Status), CreatedAt: existing.CreatedAt}, false
			return c.err
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("fetching open report: %w", err)
		}
		recent, err := c.q.HasRecentDismissedReport(ctx, queries.HasRecentDismissedReportParams{
			ReporterUserID: report.ReporterUserID, LinkID: linkID,
			Since: nullTime(report.Now.Add(-domain.LinkReportCooldown)),
		})
		if err != nil {
			return fmt.Errorf("checking for a recently dismissed report: %w", err)
		}
		if recent {
			return datasources.ErrReportCooldown
		}
		open, err := c.q.CountOpenLinkReportsOfReporter(ctx, report.ReporterUserID)
		if err != nil {
			return fmt.Errorf("counting open reports: %w", err)
		}
		if open >= domain.MaxOpenLinkReports {
			return datasources.ErrReportLimitReached
		}
		id, err := c.q.InsertLinkReport(ctx, queries.InsertLinkReportParams{
			LinkID: linkID, ReporterUserID: report.ReporterUserID, Reason: queries.LinkReportsReason(report.Reason),
			Message: nullString(report.Message), CreatedAt: report.Now,
		})
		if err != nil {
			return fmt.Errorf("inserting link report: %w", err)
		}
		receipt, created = domain.LinkReportReceipt{ID: id, Status: domain.LinkReportStatusOpen,
			CreatedAt: report.Now}, true
		return nil
	})
	if err != nil {
		return domain.LinkReportReceipt{}, false, databaseError("reporting link", err)
	}

	return receipt, created, nil
}

// ListLinkReports lists one page of link reports, oldest first, then by ID, each with its link as it is now.
func (r *Repository) ListLinkReports(
	ctx context.Context, query datasources.LinkReportQuery,
) (domain.LinkReportList, error) {
	var status queries.NullLinkReportsStatus
	if query.Status != nil {
		status = queries.NullLinkReportsStatus{LinkReportsStatus: queries.LinkReportsStatus(*query.Status), Valid: true}
	}
	var list domain.LinkReportList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		ids := fetch(ctx, c, "link reports", c.q.ListLinkReports, queries.ListLinkReportsParams{
			Status: status, Limit: query.Page.Limit, Offset: query.Page.Offset,
		})
		list.Reports = make([]domain.LinkReport, 0, len(ids))
		for _, id := range ids {
			report, err := c.linkReport(ctx, id)
			if err != nil {
				return err
			}
			list.Reports = append(list.Reports, report)
		}
		list.Total = fetch(ctx, c, "link report count", c.q.CountLinkReports, queries.CountLinkReportsParams{Status: status})
		return c.err
	})
	if err != nil {
		return domain.LinkReportList{}, databaseError("listing link reports", err)
	}

	return list, nil
}

// ResolveLinkReport resolves or dismisses an open link report. It locks the moderator's users row (lockModerator),
// reads the report's link without a lock, locks the link row, then the report row, and checks the report is still
// open; with HideLink it then hides the link, resolving its other open reports. It returns the report as the
// transaction leaves it.
func (r *Repository) ResolveLinkReport(
	ctx context.Context, req datasources.ResolveLinkReportRequest,
) (domain.LinkReport, error) {
	var report domain.LinkReport
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		id, ok := positiveID(req.ReportID)
		if !ok {
			return datasources.ErrNotFound
		}
		if err := c.lockModerator(ctx, req.ModeratorUserID); err != nil {
			return err
		}
		linkID, err := c.q.GetLinkReportLinkID(ctx, id)
		if err != nil {
			return lookupError("link report", err)
		}
		if _, err := c.q.LockLink(ctx, linkID); err != nil {
			return lookupError("link", err)
		}
		status, err := c.q.LockLinkReport(ctx, id)
		if err != nil {
			return lookupError("link report", err)
		}
		if status != queries.LinkReportsStatusOpen {
			return datasources.ErrReportNotOpen
		}
		err = requireMatched(c.q.ResolveLinkReport(ctx, queries.ResolveLinkReportParams{
			Status: queries.LinkReportsStatus(req.Status), ResolvedByUserID: nullString(&req.ModeratorUserID),
			ResolvedAt: nullTime(req.Now), ResolutionNote: nullString(req.Note), ID: id,
		}))
		if err != nil {
			return fmt.Errorf("resolving link report: %w", err)
		}
		if req.HideLink {
			if err := c.setLinkHidden(ctx, linkID, true, req.ModeratorUserID, req.Now); err != nil {
				return err
			}
		}
		report, err = c.linkReport(ctx, id)
		return err
	})
	if err != nil {
		return domain.LinkReport{}, databaseError(fmt.Sprintf("resolving link report [%d]", req.ReportID), err)
	}

	return report, nil
}

// SetLinkHidden hides or shows a link: it locks the moderator's users row (lockModerator), then the link row, sets
// its hidden state and, when hiding it, resolves its open reports. It returns the link as the transaction leaves it.
func (r *Repository) SetLinkHidden(
	ctx context.Context, req datasources.SetLinkHiddenRequest,
) (domain.ModerationLink, error) {
	var link domain.ModerationLink
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		id, ok := positiveID(req.LinkID)
		if !ok {
			return datasources.ErrNotFound
		}
		if err := c.lockModerator(ctx, req.ModeratorUserID); err != nil {
			return err
		}
		if _, err := c.q.LockLink(ctx, id); err != nil {
			return lookupError("link", err)
		}
		if err := c.setLinkHidden(ctx, id, req.Hidden, req.ModeratorUserID, req.Now); err != nil {
			return err
		}
		var err error
		link, err = c.moderationLink(ctx, id)
		return err
	})
	if err != nil {
		return domain.ModerationLink{}, databaseError(fmt.Sprintf("setting link [%d] hidden", req.LinkID), err)
	}

	return link, nil
}

// setLinkHidden sets the hidden state of the link id, whose row the transaction has locked; hiding it resolves its
// open reports for moderatorID at now with domain.LinkHiddenNote.
func (c *catalogueReader) setLinkHidden(
	ctx context.Context, id uint64, hidden bool, moderatorID string, now time.Time,
) error {
	if err := requireMatched(c.q.SetLinkHidden(ctx, queries.SetLinkHiddenParams{IsHidden: hidden, ID: id})); err != nil {
		return fmt.Errorf("setting link hidden: %w", err)
	}
	if !hidden {
		return nil
	}
	note := domain.LinkHiddenNote
	err := c.q.ResolveOpenLinkReportsOfLink(ctx, queries.ResolveOpenLinkReportsOfLinkParams{
		ResolvedByUserID: nullString(&moderatorID), ResolvedAt: nullTime(now), ResolutionNote: nullString(&note),
		LinkID: id,
	})
	if err != nil {
		return fmt.Errorf("resolving the link's open reports: %w", err)
	}

	return nil
}

// linkReport returns the link report id with its link as it is now.
func (c *catalogueReader) linkReport(ctx context.Context, id uint64) (domain.LinkReport, error) {
	row, err := c.q.GetLinkReport(ctx, id)
	if err != nil {
		return domain.LinkReport{}, lookupError("link report", err)
	}
	link, err := c.moderationLink(ctx, row.LinkID)
	if err != nil {
		return domain.LinkReport{}, err
	}
	reporter, err := moderationAuthor(row.ReporterUserID, row.ReporterDisplayName, row.ReporterRole,
		row.ReporterBlockedAt)
	if err != nil {
		return domain.LinkReport{}, err
	}
	resolver, err := optionalModerationAuthor(row.ResolvedByUserID, row.ResolverDisplayName, row.ResolverRole,
		row.ResolverBlockedAt)
	if err != nil {
		return domain.LinkReport{}, err
	}
	report := domain.LinkReport{
		ID: c.signedID(row.ID), Link: link, Reason: enumOf[domain.LinkReportReason](c, row.Reason),
		Message: stringPtr(row.Message), Reporter: reporter, Status: enumOf[domain.LinkReportStatus](c, row.Status),
		CreatedAt: row.CreatedAt, ResolvedAt: timePtr(row.ResolvedAt), ResolvedBy: resolver,
		Note: stringPtr(row.ResolutionNote),
	}

	return report, c.err
}
