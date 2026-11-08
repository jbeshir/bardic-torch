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

var _ datasources.EditableDocumentGetter = (*Repository)(nil)

// GetEditable returns the document the actor starts an edit of the entity key addresses from: the author
// document of their pending revision of it on that revision's submitted base, or else the projection of its
// current document on the current revision. Either is without what the actor may not see; an interactive
// moderator sees everything, with the links' states.
func (r *Repository) GetEditable(
	ctx context.Context, actor domain.Actor, key domain.EntityKey,
) (domain.Editable, error) {
	var editable domain.Editable
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		var err error
		editable, err = c.editable(ctx, actor, key)
		if err != nil {
			return err
		}
		return c.err
	})
	if err != nil {
		return domain.Editable{}, fmt.Errorf("getting editable %s: %w", key.Type, err)
	}

	return editable, nil
}

func (c *catalogueReader) editable(ctx context.Context, actor domain.Actor, key domain.EntityKey) (
	domain.Editable, error,
) {
	kind, err := kindFor(key.Type)
	if err != nil {
		return domain.Editable{}, err
	}
	pending := c.referencedPending(ctx, actor)
	target, err := c.editTarget(ctx, key, c.pendingNewEntities(ctx, actor.UserID, false))
	if err != nil {
		return domain.Editable{}, err
	}
	id := target.entity.ID
	editable := domain.Editable{Entity: target.entity}

	start, err := c.pendingStart(ctx, actor, kind, target, pending)
	if err != nil {
		return domain.Editable{}, err
	}
	if start == nil {
		if target.currentRevision == nil {
			return domain.Editable{}, errNotEditable
		}
		current, err := c.approvedDocument(ctx, kind, target.entity, *target.currentRevision)
		if err != nil {
			return domain.Editable{}, err
		}
		vis := c.editorVisibility(ctx, actor, id, kind, pending, current)
		editable.Document, _ = kind.editable(id, current, vis)
		editable.BaseRevisionID, editable.StartingPoint = target.currentRevision, domain.StartingPointCurrent
	} else {
		editable.Document, editable.BaseRevisionID, editable.StartingPoint = start.document, start.base,
			domain.StartingPointPending
		editable.Pending = &domain.PendingRef{ID: start.revisionID, SubmittedAt: start.submittedAt}
	}

	var refs domain.SnapshotReferences
	kind.addReferences(id, editable.Document, &refs)
	editable.References = c.editorReferences(ctx, actor, refs, pending)
	editable.LinkStates = c.documentLinkStates(ctx, actor, id, kind, editable.Document)
	draft, err := c.q.GetUserDraftForEntity(ctx, queries.GetUserDraftForEntityParams{
		AuthorUserID: nullString(&actor.UserID), EntityType: queries.RevisionsEntityType(key.Type),
		EntityID: nullableID(&id),
	})
	switch {
	case err == nil:
		editable.Draft = &domain.DraftRef{ID: c.signedID(draft.ID), UpdatedAt: draft.UpdatedAt.Time}
	case !errors.Is(err, sql.ErrNoRows):
		return domain.Editable{}, fmt.Errorf("fetching draft: %w", err)
	}

	return editable, nil
}

// pendingStart is the starting point an actor's pending revision of an entity gives an edit of it.
type pendingStart struct {
	revisionID  int64
	submittedAt time.Time
	document    domain.AnyDocument
	base        *int64
	received    domain.ElementKeySet
}

// pendingStart returns the starting point the actor's pending revision of the entity target gives: its author
// document, on its submitted base, with its received keys; or nil if the actor has no pending revision of it.
func (c *catalogueReader) pendingStart(
	ctx context.Context, actor domain.Actor, kind documentKind, target editTarget, pending []pendingNewEntity,
) (*pendingStart, error) {
	row, found, err := c.userPendingFor(ctx, actor.UserID, queries.RevisionsEntityType(target.entity.Type),
		nullableID(&target.entity.ID))
	if !found || err != nil {
		return nil, err
	}
	submitted, err := kind.decode(row.SubmittedSnapshotJson)
	if err != nil {
		return nil, fmt.Errorf("pending revision [%d] submitted snapshot: %w", row.ID, err)
	}
	received, err := decodeJSONColumn[domain.ElementKeySet]("received keys", row.ReceivedKeysJson)
	if err != nil {
		return nil, fmt.Errorf("pending revision [%d]: %w", row.ID, err)
	}
	vis := c.editorVisibility(ctx, actor, target.entity.ID, kind, pending, submitted)

	return &pendingStart{
		revisionID: c.signedID(row.ID), submittedAt: row.SubmittedAt.Time,
		document: kind.author(target.entity.ID, submitted, received, vis), base: int64Ptr(row.SubmittedBaseRevisionID),
		received: received,
	}, nil
}

// approvedDocument returns the document of revisionID, which must be an approved revision of entity.
func (c *catalogueReader) approvedDocument(
	ctx context.Context, kind documentKind, entity domain.RevisionEntity, revisionID int64,
) (domain.AnyDocument, error) {
	key, ok := positiveID(revisionID)
	if !ok {
		return nil, fmt.Errorf("revision [%d]: %w", revisionID, datasources.ErrNotFound)
	}
	row, err := c.q.GetApprovedEntityRevision(ctx, queries.GetApprovedEntityRevisionParams{
		ID: key, EntityType: queries.RevisionsEntityType(entity.Type), EntityID: nullableID(&entity.ID),
	})
	if err != nil {
		return nil, fmt.Errorf("fetching approved revision [%d] of %s [%d]: %w", revisionID, entity.Type, entity.ID,
			lookupError("revision", err))
	}
	doc, err := kind.decode(row.SnapshotJson)
	if err != nil {
		return nil, fmt.Errorf("revision [%d] snapshot: %w", revisionID, err)
	}

	return doc, nil
}

// nullableInt64 is a nullable ID column's value as a pointer, nil for NULL.
func int64Ptr(id sql.NullInt64) *int64 {
	if !id.Valid {
		return nil
	}

	return &id.Int64
}
