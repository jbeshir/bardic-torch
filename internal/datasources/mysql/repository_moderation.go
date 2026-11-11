package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ datasources.Moderation = (*Repository)(nil)

// InsertLinkReport stores a report of a visible link, refusing a second open report of it by the same user.
func (r *Repository) InsertLinkReport(ctx context.Context, report datasources.NewLinkReport) (int64, error) {
	var id int64
	err := r.inTransaction(ctx, func(q *queries.Queries) error {
		linkID, err := q.GetVisibleLinkID(ctx, report.NormalizedURL)
		if errors.Is(err, sql.ErrNoRows) {
			return datasources.ErrNotFound
		} else if err != nil {
			return err
		}
		exists, err := q.HasOpenLinkReport(ctx, queries.HasOpenLinkReportParams{
			ReporterUserID: report.ReporterUserID, LinkID: linkID,
		})
		if err != nil {
			return err
		}
		if exists {
			return datasources.ErrAlreadyReported
		}
		res, err := q.InsertLinkReport(ctx, queries.InsertLinkReportParams{
			LinkID: linkID, ReporterUserID: report.ReporterUserID, Reason: queries.LinkReportsReason(report.Reason),
			Message: nullString(report.Message), CreatedAt: report.Now,
		})
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("inserting link report: %w", err)
	}

	return id, nil
}

// ListLinkReports lists one page of the reports with the status, oldest first.
func (r *Repository) ListLinkReports(
	ctx context.Context, status domain.ReportStatus, page domain.Page,
) (domain.LinkReportList, error) {
	rows, err := r.queries.ListLinkReports(ctx, queries.ListLinkReportsParams{
		Status: queries.LinkReportsStatus(status), Limit: page.Limit, Offset: page.Offset,
	})
	if err != nil {
		return domain.LinkReportList{}, fmt.Errorf("listing link reports: %w", unavailableIfDisconnected(err))
	}
	total, err := r.queries.CountLinkReports(ctx, queries.LinkReportsStatus(status))
	if err != nil {
		return domain.LinkReportList{}, fmt.Errorf("counting link reports: %w", unavailableIfDisconnected(err))
	}

	list := domain.LinkReportList{Reports: make([]domain.LinkReport, 0, len(rows)), Total: total}
	for _, row := range rows {
		list.Reports = append(list.Reports, domain.LinkReport{
			ID:       row.ID,
			Link:     domain.ReportedLink{ID: row.LinkID, URL: row.Url, IsHidden: row.IsHidden},
			Reporter: domain.ModerationUser{UserID: row.ReporterUserID, DisplayName: row.ReporterDisplayName},
			Reason:   domain.LinkReportReason(row.Reason), Message: stringPtr(row.Message),
			Status: domain.ReportStatus(row.Status), CreatedAt: row.CreatedAt,
			Resolution: resolution(row.ResolvedByUserID, row.ResolvedAt, row.ResolutionNote),
		})
	}

	return list, nil
}

// resolution is how a report was closed, or nil if it is open.
func resolution(by sql.NullString, at sql.NullTime, note sql.NullString) *domain.Resolution {
	if !by.Valid || !at.Valid {
		return nil
	}

	return &domain.Resolution{ResolvedBy: by.String, ResolvedAt: at.Time, Note: stringPtr(note)}
}

// CloseLinkReport resolves or dismisses an open report, hiding its link too if hideLink.
func (r *Repository) CloseLinkReport(ctx context.Context, closing datasources.Closing, hideLink bool) error {
	id := closing.ID
	err := r.inTransaction(ctx, func(q *queries.Queries) error {
		if id < 1 {
			return datasources.ErrNotFound
		}
		state, err := q.GetLinkReportState(ctx, uint64(id))
		if errors.Is(err, sql.ErrNoRows) {
			return datasources.ErrNotFound
		} else if err != nil {
			return err
		}
		if state.Status != queries.LinkReportsStatusOpen {
			return datasources.ErrNotOpen
		}
		err = q.CloseLinkReport(ctx, queries.CloseLinkReportParams{
			Status: queries.LinkReportsStatus(closing.Status), ResolvedByUserID: nullString(&closing.ModeratorUserID),
			ResolvedAt: nullTime(closing.Now), ResolutionNote: nullString(closing.Note), ID: uint64(id),
		})
		if err != nil || !hideLink {
			return err
		}

		return requireMatched(q.SetLinkHidden(ctx, queries.SetLinkHiddenParams{IsHidden: true, ID: state.LinkID}))
	})
	if err != nil {
		return fmt.Errorf("closing link report [%d]: %w", closing.ID, err)
	}

	return nil
}

// GetModerationLink returns the link with the normalized URL, hidden or not, with its attachments.
func (r *Repository) GetModerationLink(ctx context.Context, normalizedURL string) (domain.ModerationLink, error) {
	row, err := r.queries.GetModerationLink(ctx, normalizedURL)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ModerationLink{}, fmt.Errorf("link: %w", datasources.ErrNotFound)
	} else if err != nil {
		return domain.ModerationLink{}, fmt.Errorf("fetching link: %w", unavailableIfDisconnected(err))
	}
	attachments, err := r.queries.ListModerationLinkAttachments(ctx, row.ID)
	if err != nil {
		return domain.ModerationLink{}, fmt.Errorf("fetching link attachments: %w", unavailableIfDisconnected(err))
	}

	link := domain.ModerationLink{ID: row.ID, URL: row.Url, Title: stringPtr(row.Title), IsHidden: row.IsHidden,
		Liveness: domain.Liveness(row.Liveness), Attachments: []domain.LinkAttachment{}}
	// The rows are ordered by entity, so an entity's roles are consecutive.
	for _, a := range attachments {
		attachment := domain.LinkAttachment{EntityType: domain.EntityTypePerson, Slug: a.PersonSlug.String,
			Title: a.PersonName.String}
		if a.PieceSlug.Valid {
			attachment = domain.LinkAttachment{EntityType: domain.EntityTypePiece, Slug: a.PieceSlug.String,
				Title: a.PieceTitle.String}
		}
		last := len(link.Attachments) - 1
		if last >= 0 && link.Attachments[last].EntityType == attachment.EntityType &&
			link.Attachments[last].Slug == attachment.Slug {
			attachment = link.Attachments[last]
			link.Attachments = link.Attachments[:last]
		}
		attachment.Roles = append(attachment.Roles, domain.LinkRole(a.Role))
		link.Attachments = append(link.Attachments, attachment)
	}

	return link, nil
}

// SetLinkHidden hides or restores a link.
func (r *Repository) SetLinkHidden(ctx context.Context, linkID int64, hidden bool) error {
	if linkID < 1 {
		return fmt.Errorf("setting link [%d] hidden: %w", linkID, datasources.ErrNotFound)
	}
	err := requireMatched(r.queries.SetLinkHidden(ctx, queries.SetLinkHiddenParams{IsHidden: hidden, ID: uint64(linkID)}))
	if err != nil {
		return fmt.Errorf("setting link [%d] hidden: %w", linkID, err)
	}

	return nil
}

// InsertContactRequest stores a contact request. Its personal fields are never named in an error.
func (r *Repository) InsertContactRequest(ctx context.Context, request datasources.NewContactRequest) (int64, error) {
	res, err := r.queries.InsertContactRequest(ctx, queries.InsertContactRequestParams{
		Kind: queries.ContactRequestsKind(request.Kind), Name: nullString(request.Name),
		Email:   sql.NullString{String: request.Email, Valid: true},
		Message: sql.NullString{String: request.Message, Valid: true}, Url: nullString(request.URL),
		CreatedAt: request.Now,
	})
	if err != nil {
		return 0, fmt.Errorf("inserting contact request: %w", unavailableIfDisconnected(err))
	}

	return res.LastInsertId()
}

// ListContactRequests lists one page of the requests with the status, oldest first.
func (r *Repository) ListContactRequests(
	ctx context.Context, status domain.ReportStatus, page domain.Page,
) (domain.ContactRequestList, error) {
	rows, err := r.queries.ListContactRequests(ctx, queries.ListContactRequestsParams{
		Status: queries.ContactRequestsStatus(status), Limit: page.Limit, Offset: page.Offset,
	})
	if err != nil {
		return domain.ContactRequestList{}, fmt.Errorf("listing contact requests: %w", unavailableIfDisconnected(err))
	}
	total, err := r.queries.CountContactRequests(ctx, queries.ContactRequestsStatus(status))
	if err != nil {
		return domain.ContactRequestList{}, fmt.Errorf("counting contact requests: %w", unavailableIfDisconnected(err))
	}

	list := domain.ContactRequestList{Requests: make([]domain.ContactRequest, 0, len(rows)), Total: total}
	for _, row := range rows {
		list.Requests = append(list.Requests, domain.ContactRequest{
			ID: row.ID, Kind: domain.ContactKind(row.Kind), Name: stringPtr(row.Name),
			Email: stringPtr(row.Email), Message: stringPtr(row.Message), URL: stringPtr(row.Url),
			Status: domain.ReportStatus(row.Status), CreatedAt: row.CreatedAt,
			Resolution: resolution(row.ResolvedByUserID, row.ResolvedAt, row.ResolutionNote),
		})
	}

	return list, nil
}

// CloseContactRequest resolves or dismisses an open contact request.
func (r *Repository) CloseContactRequest(ctx context.Context, closing datasources.Closing) error {
	id := closing.ID
	err := r.inTransaction(ctx, func(q *queries.Queries) error {
		if id < 1 {
			return datasources.ErrNotFound
		}
		status, err := q.GetContactRequestStatus(ctx, uint64(id))
		if errors.Is(err, sql.ErrNoRows) {
			return datasources.ErrNotFound
		} else if err != nil {
			return err
		}
		if status != queries.ContactRequestsStatusOpen {
			return datasources.ErrNotOpen
		}

		return q.CloseContactRequest(ctx, queries.CloseContactRequestParams{
			Status: queries.ContactRequestsStatus(closing.Status), ResolvedByUserID: nullString(&closing.ModeratorUserID),
			ResolvedAt: nullTime(closing.Now), ResolutionNote: nullString(closing.Note), ID: uint64(id),
		})
	})
	if err != nil {
		return fmt.Errorf("closing contact request [%d]: %w", closing.ID, err)
	}

	return nil
}
