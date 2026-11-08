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

// The steps the writes that create a pending revision (a submission and a revert) and the approval share, so that
// each rule is kept once.

// checkPendingCap returns datasources.ErrPendingLimitReached if author already has domain.MaxOpenPendingRevisions
// pending revisions, not counting the revision excluded, which the new one supersedes (0 for none).
func (c *catalogueReader) checkPendingCap(ctx context.Context, author sql.NullString, excluded uint64) error {
	count, err := c.q.CountPendingRevisions(ctx, queries.CountPendingRevisionsParams{
		AuthorUserID: author, ExcludedID: excluded,
	})
	if err != nil {
		return fmt.Errorf("counting pending revisions: %w", err)
	}
	if count >= domain.MaxOpenPendingRevisions {
		return datasources.ErrPendingLimitReached
	}

	return nil
}

// withdrawSuperseded withdraws the pending revision id that a new pending revision supersedes; 0 is none.
func (c *catalogueReader) withdrawSuperseded(ctx context.Context, id uint64) error {
	if id == 0 {
		return nil
	}
	if err := requireMatched(c.q.WithdrawRevision(ctx, id)); err != nil {
		return fmt.Errorf("superseding revision [%d]: %w", id, err)
	}

	return nil
}

// userPendingFor returns the user's newest pending revision of the entity, read without a lock, and whether there
// is one.
func (c *catalogueReader) userPendingFor(
	ctx context.Context, user string, entityType queries.RevisionsEntityType, entityID sql.NullInt64,
) (queries.GetUserPendingForEntityRow, bool, error) {
	pending, err := c.q.GetUserPendingForEntity(ctx, queries.GetUserPendingForEntityParams{
		AuthorUserID: nullString(&user), EntityType: entityType, EntityID: entityID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return queries.GetUserPendingForEntityRow{}, false, nil
	}
	if err != nil {
		return queries.GetUserPendingForEntityRow{}, false, fmt.Errorf("fetching pending revision: %w", err)
	}

	return pending, true, nil
}

// hiddenLinkURLs returns the normalized URLs of the hidden links among those refs references.
func (c *catalogueReader) hiddenLinkURLs(ctx context.Context, refs domain.SnapshotReferences) map[string]bool {
	hidden := map[string]bool{}
	for _, state := range c.linkStates(ctx, refs.LinkURLs) {
		if state.isHidden {
			hidden[state.normalizedURL] = true
		}
	}

	return hidden
}
