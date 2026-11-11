package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ datasources.Revisions = (*Repository)(nil)

// inTransaction runs f with the queries of one transaction, and commits it if f succeeds.
func (r *Repository) inTransaction(ctx context.Context, f func(q *queries.Queries) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", unavailableIfDisconnected(err))
	}
	defer func() { _ = tx.Rollback() }()

	if err := f(r.queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}

// SubmitRevision stores a revision, in one transaction: a contributor's under a lock of their user row, after
// withdrawing their pending revision of the same entity; a moderator's approved at once.
func (r *Repository) SubmitRevision(ctx context.Context, rev datasources.NewRevision) (int64, error) {
	entityType, err := documentEntityType(rev.Document)
	if err != nil {
		return 0, err
	}
	snapshot, err := json.Marshal(rev.Document)
	if err != nil {
		return 0, fmt.Errorf("encoding the document: %w", err)
	}

	var id int64
	err = r.inTransaction(ctx, func(q *queries.Queries) error {
		var entityID sql.NullInt64
		if rev.BaseRevisionID != nil {
			if *rev.BaseRevisionID < 1 {
				return datasources.ErrNotFound
			}
			entityID, err = q.GetApprovedRevisionEntity(ctx, queries.GetApprovedRevisionEntityParams{
				ID: uint64(*rev.BaseRevisionID), EntityType: entityType})
			if err != nil {
				return lookupError("base revision", err)
			}
		}
		author := sql.NullString{String: rev.AuthorUserID, Valid: true}
		if !rev.ByModerator {
			if err := checkPendingLimit(ctx, q, author, entityType, entityID); err != nil {
				return err
			}
		}
		id, err = q.InsertRevision(ctx, queries.InsertRevisionParams{EntityType: entityType, EntityID: entityID,
			BaseRevisionID: nullableID(rev.BaseRevisionID), AuthorUserID: author, SnapshotJson: snapshot,
			Summary: rev.Summary, CreatedAt: rev.At})
		if err != nil {
			return fmt.Errorf("inserting revision: %w", err)
		}
		if rev.ByModerator {
			return approve(ctx, q, id, rev.AuthorUserID, rev.AcknowledgeStale, rev.At)
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("submitting %s revision: %w", entityType, err)
	}

	return id, nil
}

// checkPendingLimit locks a contributor's user row, withdraws their pending revision of the entity entityID (none
// for a new entity), and returns datasources.ErrPendingLimit if they still have MaxPendingRevisions pending.
func checkPendingLimit(
	ctx context.Context, q *queries.Queries, author sql.NullString, entityType queries.RevisionsEntityType,
	entityID sql.NullInt64,
) error {
	if _, err := q.LockUser(ctx, author.String); err != nil {
		return lookupError("user", err)
	}
	if entityID.Valid {
		err := q.WithdrawPendingRevisionsOfEntity(ctx, queries.WithdrawPendingRevisionsOfEntityParams{
			AuthorUserID: author, EntityType: entityType, EntityID: entityID})
		if err != nil {
			return fmt.Errorf("withdrawing the earlier revision: %w", err)
		}
	}
	pending, err := q.CountAuthorRevisions(ctx, queries.CountAuthorRevisionsParams{AuthorUserID: author,
		Status: queries.NullRevisionsStatus{RevisionsStatus: queries.RevisionsStatusPending, Valid: true}})
	if err != nil {
		return fmt.Errorf("counting pending revisions: %w", err)
	}
	if pending >= datasources.MaxPendingRevisions {
		return datasources.ErrPendingLimit
	}

	return nil
}

// WithdrawRevision withdraws a pending revision of its author.
func (r *Repository) WithdrawRevision(ctx context.Context, id int64, userID string) error {
	err := r.inTransaction(ctx, func(q *queries.Queries) error {
		lock, err := lockRevision(ctx, q, id)
		if err != nil {
			return err
		}
		if lock.AuthorUserID.String != userID || !lock.AuthorUserID.Valid {
			return datasources.ErrNotFound
		}
		if err := domain.CheckPending(domain.RevisionStatus(lock.Status)); err != nil {
			return err
		}
		return setStatus(ctx, q, lock.ID, queries.UpdateRevisionStatusParams{Status: queries.RevisionsStatusWithdrawn})
	})
	if err != nil {
		return fmt.Errorf("withdrawing revision [%d]: %w", id, err)
	}

	return nil
}

// RejectRevision rejects a pending revision with a note.
func (r *Repository) RejectRevision(ctx context.Context, id int64, reviewerUserID, note string, at time.Time) error {
	err := r.inTransaction(ctx, func(q *queries.Queries) error {
		lock, err := lockRevision(ctx, q, id)
		if err != nil {
			return err
		}
		if err := domain.CheckPending(domain.RevisionStatus(lock.Status)); err != nil {
			return err
		}
		return setStatus(ctx, q, lock.ID, queries.UpdateRevisionStatusParams{Status: queries.RevisionsStatusRejected,
			ReviewerUserID: sql.NullString{String: reviewerUserID, Valid: true}, ReviewedAt: nullTime(at),
			ReviewNote: sql.NullString{String: note, Valid: true}})
	})
	if err != nil {
		return fmt.Errorf("rejecting revision [%d]: %w", id, err)
	}

	return nil
}

// ApproveRevision approves a pending revision and writes its document into the catalogue, in one transaction.
func (r *Repository) ApproveRevision(
	ctx context.Context, id int64, reviewerUserID string, acknowledgeStale bool, at time.Time,
) error {
	err := r.inTransaction(ctx, func(q *queries.Queries) error {
		return approve(ctx, q, id, reviewerUserID, acknowledgeStale, at)
	})
	if err != nil {
		return fmt.Errorf("approving revision [%d]: %w", id, err)
	}

	return nil
}

// approve approves the pending revision id: it writes the document into its entity, creating a new entity's row,
// and marks the revision approved.
func approve(
	ctx context.Context, q *queries.Queries, id int64, reviewerUserID string, acknowledgeStale bool, at time.Time,
) error {
	row, doc, err := approvable(ctx, q, id, acknowledgeStale)
	if err != nil {
		return err
	}
	w := &entityWriter{ctx: ctx, q: q}
	entityID := w.write(row.EntityID.Int64, doc, approval{revisionID: id, summary: row.Summary, at: at})
	if w.err != nil {
		return w.err
	}
	if !row.EntityID.Valid {
		err := q.SetRevisionEntity(ctx, queries.SetRevisionEntityParams{EntityID: someID(entityID), ID: row.ID})
		if err != nil {
			return fmt.Errorf("setting the revision's entity: %w", err)
		}
	}

	return setStatus(ctx, q, row.ID, queries.UpdateRevisionStatusParams{Status: queries.RevisionsStatusApproved,
		ReviewerUserID: sql.NullString{String: reviewerUserID, Valid: true}, ReviewedAt: nullTime(at)})
}

// approvable locks the revision id and returns it with its document if it may be approved: it is pending, not
// stale unless acknowledgeStale, and its document still validates.
func approvable(
	ctx context.Context, q *queries.Queries, id int64, acknowledgeStale bool,
) (queries.GetRevisionRow, document, error) {
	lock, err := lockRevision(ctx, q, id)
	if err != nil {
		return queries.GetRevisionRow{}, nil, err
	}
	if err := domain.CheckPending(domain.RevisionStatus(lock.Status)); err != nil {
		return queries.GetRevisionRow{}, nil, err
	}
	row, err := getRevision(ctx, q, id)
	if err != nil {
		return queries.GetRevisionRow{}, nil, err
	}
	current, err := currentRevisionID(ctx, q, row.EntityType, row.EntityID)
	if err != nil {
		return queries.GetRevisionRow{}, nil, err
	}
	if domain.IsStale(int64Ptr(row.BaseRevisionID), current) && !acknowledgeStale {
		return queries.GetRevisionRow{}, nil, datasources.ErrStale
	}
	doc, err := decodeDocument(row.EntityType, row.SnapshotJson)
	if err != nil {
		return queries.GetRevisionRow{}, nil, err
	}
	vctx, err := validationContext(ctx, q, row.EntityID.Int64)
	if err != nil {
		return queries.GetRevisionRow{}, nil, err
	}
	if problems := doc.Validate(vctx); len(problems) > 0 {
		return queries.GetRevisionRow{}, nil, &datasources.ValidationError{Problems: problems}
	}

	return row, doc, nil
}

// lockRevision locks the revision id FOR UPDATE, returning datasources.ErrNotFound if there is none.
func lockRevision(ctx context.Context, q *queries.Queries, id int64) (queries.LockRevisionRow, error) {
	if id < 1 {
		return queries.LockRevisionRow{}, datasources.ErrNotFound
	}
	lock, err := q.LockRevision(ctx, uint64(id))
	if err != nil {
		return queries.LockRevisionRow{}, lookupError("revision", err)
	}

	return lock, nil
}

// setStatus sets the status and review fields of the revision id.
func setStatus(ctx context.Context, q *queries.Queries, id uint64, p queries.UpdateRevisionStatusParams) error {
	p.ID = id
	if err := q.UpdateRevisionStatus(ctx, p); err != nil {
		return fmt.Errorf("setting revision status: %w", err)
	}

	return nil
}

// documentEntityType returns the entity type of a document.
func documentEntityType(doc any) (queries.RevisionsEntityType, error) {
	switch doc.(type) {
	case domain.PieceDocument:
		return queries.RevisionsEntityTypePiece, nil
	case domain.PersonDocument:
		return queries.RevisionsEntityTypePerson, nil
	case domain.TermDocument:
		return queries.RevisionsEntityTypeTerm, nil
	case domain.CollectionDocument:
		return queries.RevisionsEntityTypeCollection, nil
	case domain.EventDocument:
		return queries.RevisionsEntityTypeEvent, nil
	default:
		return "", fmt.Errorf("%w: %T", errUnknownDocument, doc)
	}
}

// document is a decoded stored document: a domain.PieceDocument, PersonDocument, TermDocument, CollectionDocument
// or EventDocument.
type document interface {
	Validate(ctx domain.ValidationContext) []domain.Problem
	DuplicateKeys() []string
}

// decodeDocument decodes a stored document of entityType.
func decodeDocument(entityType queries.RevisionsEntityType, data []byte) (document, error) {
	var doc document
	var err error
	switch entityType {
	case queries.RevisionsEntityTypePiece:
		doc, err = domain.DecodeDocument[domain.PieceDocument](data)
	case queries.RevisionsEntityTypePerson:
		doc, err = domain.DecodeDocument[domain.PersonDocument](data)
	case queries.RevisionsEntityTypeTerm:
		doc, err = domain.DecodeDocument[domain.TermDocument](data)
	case queries.RevisionsEntityTypeCollection:
		doc, err = domain.DecodeDocument[domain.CollectionDocument](data)
	case queries.RevisionsEntityTypeEvent:
		doc, err = domain.DecodeDocument[domain.EventDocument](data)
	default:
		return nil, fmt.Errorf("%w [%s] of revision entity type", domain.ErrUnknownEnumValue, entityType)
	}
	if err != nil {
		return nil, fmt.Errorf("snapshot: %w", err)
	}

	return doc, nil
}
