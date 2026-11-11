package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ datasources.LinkChecks = (*Repository)(nil)

// ListLinksDueForCheck returns up to limit visible links never checked or last checked before checkedBefore.
func (r *Repository) ListLinksDueForCheck(
	ctx context.Context, checkedBefore time.Time, limit int32,
) ([]datasources.DueLink, error) {
	rows, err := r.queries.ListLinksDueForCheck(ctx, queries.ListLinksDueForCheckParams{
		CheckedBefore: nullTime(checkedBefore), Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("listing links due for a check: %w", err)
	}
	links := make([]datasources.DueLink, 0, len(rows))
	for _, row := range rows {
		links = append(links, datasources.DueLink{
			ID: row.ID, URL: row.Url, Liveness: domain.Liveness(row.Liveness), Failures: row.LivenessFailures,
		})
	}

	return links, nil
}

// UpdateLinkCheck records the result of a check.
func (r *Repository) UpdateLinkCheck(
	ctx context.Context, id uint64, liveness domain.Liveness, failures uint8, checkedAt time.Time,
) error {
	err := r.queries.UpdateLinkCheck(ctx, queries.UpdateLinkCheckParams{
		Liveness: queries.LinksLiveness(liveness), LivenessFailures: failures, CheckedAt: nullTime(checkedAt), ID: id,
	})
	if err != nil {
		return fmt.Errorf("updating link check: %w", err)
	}

	return nil
}

// ListLinksDueForArchive returns up to limit visible links whose archived copy is due to be looked up.
func (r *Repository) ListLinksDueForArchive(
	ctx context.Context, checkedBefore time.Time, limit int32,
) ([]datasources.DueLink, error) {
	rows, err := r.queries.ListLinksDueForArchive(ctx, queries.ListLinksDueForArchiveParams{
		CheckedBefore: nullTime(checkedBefore), Limit: limit,
	})
	if err != nil {
		return nil, fmt.Errorf("listing links due for an archive lookup: %w", err)
	}
	links := make([]datasources.DueLink, 0, len(rows))
	for _, row := range rows {
		links = append(links, datasources.DueLink{ID: row.ID, URL: row.Url})
	}

	return links, nil
}

// UpdateLinkArchive records an archive lookup, keeping an archived copy the link already has.
func (r *Repository) UpdateLinkArchive(
	ctx context.Context, id uint64, archivedURL string, checkedAt time.Time,
) error {
	err := r.queries.UpdateLinkArchive(ctx, queries.UpdateLinkArchiveParams{
		ArchivedUrl: sql.NullString{String: archivedURL, Valid: archivedURL != ""}, CheckedAt: nullTime(checkedAt), ID: id,
	})
	if err != nil {
		return fmt.Errorf("updating link archive: %w", err)
	}

	return nil
}
