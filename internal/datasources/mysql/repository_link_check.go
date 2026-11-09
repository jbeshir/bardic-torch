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

var _ datasources.LinkChecker = (*Repository)(nil)

// ListLinksDueForCheck returns up to limit visible links never checked or last checked before checkedBefore.
func (r *Repository) ListLinksDueForCheck(
	ctx context.Context, checkedBefore time.Time, limit int32,
) ([]datasources.LinkCheckTarget, error) {
	var targets []datasources.LinkCheckTarget
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		rows := fetch(ctx, c, "links due for a check", c.q.ListLinksDueForCheck, queries.ListLinksDueForCheckParams{
			CheckedBefore: nullTime(checkedBefore), Limit: limit,
		})
		targets = make([]datasources.LinkCheckTarget, 0, len(rows))
		for _, row := range rows {
			targets = append(targets, datasources.LinkCheckTarget{
				ID: c.signedID(row.ID), URL: row.Url, Liveness: enumOf[domain.Liveness](c, row.Liveness),
				Failures: row.LivenessFailures, LastCheckedAt: timePtr(row.LastCheckedAt),
			})
		}

		return c.err
	})
	if err != nil {
		return nil, databaseError("listing links due for a check", err)
	}

	return targets, nil
}

// UpdateLinkCheck records a result if the link is visible and its URL and last check time are the ones expected.
func (r *Repository) UpdateLinkCheck(ctx context.Context, result datasources.LinkCheckResult) (bool, error) {
	id, ok := positiveID(result.ID)
	if !ok {
		return false, nil
	}
	var written bool
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var expected sql.NullTime
		if result.ExpectedLastCheckedAt != nil {
			expected = nullTime(*result.ExpectedLastCheckedAt)
		}
		rows, err := r.queries.WithTx(tx).UpdateLinkCheck(ctx, queries.UpdateLinkCheckParams{
			Liveness: queries.LinksLiveness(result.Liveness), LivenessFailures: result.Failures,
			CheckedAt: nullTime(result.CheckedAt), ID: id, Url: result.ProbedURL, ExpectedLastCheckedAt: expected,
		})
		if err != nil {
			return fmt.Errorf("updating link check: %w", err)
		}
		written = rows > 0

		return nil
	})
	if err != nil {
		return false, databaseError("recording a link check", err)
	}

	return written, nil
}

// ListLinksDueForArchive returns up to limit visible links whose archived copy is due to be looked up.
func (r *Repository) ListLinksDueForArchive(
	ctx context.Context, checkedBefore time.Time, limit int32,
) ([]datasources.LinkArchiveTarget, error) {
	var targets []datasources.LinkArchiveTarget
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		rows := fetch(ctx, c, "links due for an archive lookup", c.q.ListLinksDueForArchive,
			queries.ListLinksDueForArchiveParams{CheckedBefore: nullTime(checkedBefore), Limit: limit})
		targets = make([]datasources.LinkArchiveTarget, 0, len(rows))
		for _, row := range rows {
			targets = append(targets, datasources.LinkArchiveTarget{ID: c.signedID(row.ID), URL: row.Url})
		}

		return c.err
	})
	if err != nil {
		return nil, databaseError("listing links due for an archive lookup", err)
	}

	return targets, nil
}

// UpdateLinkArchive records an archive lookup of a visible link, keeping an archived copy it already has.
func (r *Repository) UpdateLinkArchive(ctx context.Context, result datasources.LinkArchiveResult) (bool, error) {
	id, ok := positiveID(result.ID)
	if !ok {
		return false, nil
	}
	var written bool
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, err := r.queries.WithTx(tx).UpdateLinkArchive(ctx, queries.UpdateLinkArchiveParams{
			ArchivedUrl: nullString(result.ArchivedURL), CheckedAt: nullTime(result.CheckedAt), ID: id,
		})
		if err != nil {
			return fmt.Errorf("updating link archive: %w", err)
		}
		written = rows > 0

		return nil
	})
	if err != nil {
		return false, databaseError("recording an archive lookup", err)
	}

	return written, nil
}
