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

var (
	_ datasources.MyRevisionLister     = (*Repository)(nil)
	_ datasources.MyRevisionGetter     = (*Repository)(nil)
	_ datasources.PendingVersionGetter = (*Repository)(nil)
)

// ListMyRevisions lists one page of the actor's own revisions, drafts included, newest first.
func (r *Repository) ListMyRevisions(
	ctx context.Context, actor domain.Actor, query datasources.MyRevisionQuery,
) (domain.MyRevisionList, error) {
	params := queries.ListMyRevisionsParams{AuthorUserID: nullString(&actor.UserID), Limit: query.Page.Limit,
		Offset: query.Page.Offset}
	if query.Status != nil {
		params.Status = queries.NullRevisionsStatus{RevisionsStatus: queries.RevisionsStatus(*query.Status), Valid: true}
	}
	if query.EntityType != nil {
		params.EntityType = queries.NullRevisionsEntityType{
			RevisionsEntityType: queries.RevisionsEntityType(*query.EntityType), Valid: true,
		}
	}

	var list domain.MyRevisionList
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		author, err := c.authorRef(ctx, actor)
		if err != nil {
			return err
		}
		rows := fetch(ctx, c, "own revisions", c.q.ListMyRevisions, params)
		list.Revisions = make([]domain.MyRevisionSummary, 0, len(rows))
		for _, row := range rows {
			summary, err := c.myRevisionSummary(ctx, author, myRevisionRow{
				id: row.ID, entityType: row.EntityType, entityID: row.EntityID, base: row.BaseRevisionID,
				submittedBase: row.SubmittedBaseRevisionID, source: row.Source, status: row.Status,
				snapshot: row.SnapshotJson, submitted: row.SubmittedSnapshotJson, summary: row.Summary,
				reviewedAt: row.ReviewedAt, reviewNote: row.ReviewNote, createdAt: row.CreatedAt,
				submittedAt: row.SubmittedAt,
			})
			if err != nil {
				return err
			}
			list.Revisions = append(list.Revisions, summary)
		}
		list.Total = fetch(ctx, c, "own revision count", c.q.CountMyRevisions, queries.CountMyRevisionsParams{
			AuthorUserID: params.AuthorUserID, Status: params.Status, EntityType: params.EntityType,
		})
		return c.err
	})
	if err != nil {
		return domain.MyRevisionList{}, fmt.Errorf("listing own revisions: %w", err)
	}

	return list, nil
}

// GetMyRevision returns one of the actor's own revisions with its views.
func (r *Repository) GetMyRevision(ctx context.Context, actor domain.Actor, id int64) (domain.MyRevision, error) {
	var revision domain.MyRevision
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		row, err := c.ownRevision(ctx, actor, id)
		if err != nil {
			return err
		}
		revision, err = c.myRevision(ctx, actor, row)
		if err != nil {
			return err
		}
		return c.err
	})
	if err != nil {
		return domain.MyRevision{}, fmt.Errorf("getting own revision [%d]: %w", id, err)
	}

	return revision, nil
}

// GetPendingVersion returns the actor's pending revision of the entity key addresses, with its views.
func (r *Repository) GetPendingVersion(
	ctx context.Context, actor domain.Actor, key domain.EntityKey,
) (domain.MyRevision, error) {
	var revision domain.MyRevision
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		target, err := c.editTarget(ctx, key, c.pendingNewEntities(ctx, actor.UserID, false))
		if err != nil {
			return err
		}
		pending, found, err := c.userPendingFor(ctx, actor.UserID, queries.RevisionsEntityType(key.Type),
			nullableID(&target.entity.ID))
		if err != nil {
			return err
		}
		if !found {
			return datasources.ErrNotFound
		}
		row, err := c.q.GetRevision(ctx, pending.ID)
		if err != nil {
			return fmt.Errorf("fetching pending revision: %w", err)
		}
		revision, err = c.myRevision(ctx, actor, row)
		if err != nil {
			return err
		}
		return c.err
	})
	if err != nil {
		return domain.MyRevision{}, fmt.Errorf("getting pending version of %s: %w", key.Type, err)
	}

	return revision, nil
}

// myRevisionRow is the columns of a revision its author's views are built from.
type myRevisionRow struct {
	id                      uint64
	entityType              queries.RevisionsEntityType
	entityID                sql.NullInt64
	base, submittedBase     sql.NullInt64
	source                  queries.RevisionsSource
	status                  queries.RevisionsStatus
	snapshot, submitted     json.RawMessage
	summary                 string
	reviewedAt, submittedAt sql.NullTime
	reviewNote              sql.NullString
	createdAt               time.Time
}

// authorDocument returns the document of the revision its author's views show: what they submitted, which a draft
// holds as its snapshot; for an approved revision the full one, whose views are the public ones.
func (row myRevisionRow) authorDocument(kind documentKind) (domain.AnyDocument, error) {
	data := row.submitted
	if row.status == queries.RevisionsStatusDraft || row.status == queries.RevisionsStatusApproved || len(data) == 0 {
		data = row.snapshot
	}
	doc, err := kind.decode(data)
	if err != nil {
		return nil, fmt.Errorf("revision [%d] snapshot: %w", row.id, err)
	}

	return doc, nil
}

// viewedBase is the base the author's views compare the revision with: the base they edited from, which a
// rebase never moves.
func (row myRevisionRow) viewedBase() sql.NullInt64 {
	if row.submittedBase.Valid {
		return row.submittedBase
	}

	return row.base
}

// authorRef returns how the actor is shown as an author.
func (c *catalogueReader) authorRef(ctx context.Context, actor domain.Actor) (domain.AuthorRef, error) {
	user, err := c.q.GetUser(ctx, actor.UserID)
	if err != nil {
		return domain.AuthorRef{}, fmt.Errorf("fetching author: %w", lookupError("user", err))
	}

	role, err := domain.ParseRole(string(user.Role))
	if err != nil {
		return domain.AuthorRef{}, err
	}

	return domain.AuthorRef{DisplayName: user.DisplayName, Role: role}, nil
}

func (c *catalogueReader) myRevisionSummary(
	ctx context.Context, author domain.AuthorRef, row myRevisionRow,
) (domain.MyRevisionSummary, error) {
	entityType := enumOf[domain.EntityType](c, row.entityType)
	kind, err := kindFor(entityType)
	if err != nil {
		return domain.MyRevisionSummary{}, err
	}
	doc, err := row.authorDocument(kind)
	if err != nil {
		return domain.MyRevisionSummary{}, err
	}
	_, entity, err := c.revisionEntity(ctx, entityType, row.entityID)
	if err != nil {
		return domain.MyRevisionSummary{}, err
	}

	return domain.MyRevisionSummary{
		ID: c.signedID(row.id), Status: enumOf[domain.RevisionStatus](c, row.status), EntityType: entityType,
		Entity: entity, Title: documentTitle(doc), Summary: row.summary,
		Source: enumOf[domain.RevisionSource](c, row.source), Author: author, CreatedAt: row.createdAt,
		SubmittedAt: timePtr(row.submittedAt), ReviewedAt: timePtr(row.reviewedAt), ReviewNote: stringPtr(row.reviewNote),
		BaseRevisionID: int64Ptr(row.base), SubmittedBaseRevisionID: int64Ptr(row.submittedBase),
	}, nil
}

// myRevision returns a revision with its author's views: of what they submitted, as their author view
// (domain.AuthorDocument and domain.AuthorVisibility), and the same construction over the base they edited from;
// or for an approved revision the public views.
func (c *catalogueReader) myRevision(
	ctx context.Context, actor domain.Actor, row queries.GetRevisionRow,
) (domain.MyRevision, error) {
	role, err := domain.ParseRole(string(row.AuthorRole.UsersRole))
	if err != nil {
		return domain.MyRevision{}, err
	}
	own := myRevisionRow{
		id: row.ID, entityType: row.EntityType, entityID: row.EntityID, base: row.BaseRevisionID,
		submittedBase: row.SubmittedBaseRevisionID, source: row.Source, status: row.Status, snapshot: row.SnapshotJson,
		submitted: row.SubmittedSnapshotJson, summary: row.Summary, reviewedAt: row.ReviewedAt,
		reviewNote: row.ReviewNote, createdAt: row.CreatedAt, submittedAt: row.SubmittedAt,
	}
	summary, err := c.myRevisionSummary(ctx, domain.AuthorRef{DisplayName: row.AuthorDisplayName.String, Role: role},
		own)
	if err != nil {
		return domain.MyRevision{}, err
	}
	kind, err := kindFor(summary.EntityType)
	if err != nil {
		return domain.MyRevision{}, err
	}
	doc, base, err := c.myRevisionDocuments(ctx, kind, own)
	if err != nil {
		return domain.MyRevision{}, err
	}
	received, err := decodeJSONColumn[domain.ElementKeySet]("received keys", row.ReceivedKeysJson)
	if err != nil {
		return domain.MyRevision{}, err
	}

	id := row.EntityID.Int64
	pending := c.referencedPending(ctx, actor)
	docs := []domain.AnyDocument{doc}
	if base != nil {
		docs = append(docs, base)
	}
	vis := c.editorVisibility(ctx, actor, id, kind, pending, docs...)
	revision := domain.MyRevision{MyRevisionSummary: summary}
	if revision.Snapshot, revision.BaseSnapshot, err = myRevisionViews(kind, id, own.status, doc, base, received,
		vis); err != nil {
		return domain.MyRevision{}, fmt.Errorf("revision [%d] views: %w", row.ID, err)
	}

	var refs domain.SnapshotReferences
	kind.addReferences(id, doc, &refs)
	revision.PendingReferences = []domain.EditorReference{}
	for _, ref := range c.editorReferences(ctx, actor, refs, ownPendingFor(actor, pending)) {
		if ref.IsPending {
			revision.PendingReferences = append(revision.PendingReferences, ref)
		}
	}

	return revision, nil
}

// myRevisionDocuments returns the document of a revision its author's views show, and the document of the base
// they compare it with, nil for a new entity.
func (c *catalogueReader) myRevisionDocuments(
	ctx context.Context, kind documentKind, row myRevisionRow,
) (domain.AnyDocument, domain.AnyDocument, error) {
	doc, err := row.authorDocument(kind)
	if err != nil {
		return nil, nil, err
	}
	baseID := row.viewedBase()
	if !baseID.Valid {
		return doc, nil, nil
	}
	entity := domain.RevisionEntity{Type: enumOf[domain.EntityType](c, row.entityType), ID: row.entityID.Int64}
	base, err := c.approvedDocument(ctx, kind, entity, baseID.Int64)

	return doc, base, err
}

// myRevisionViews returns the views of a revision of status and of its base (nil for none) for its author: of
// an approved revision the public views, and otherwise the author's view of what they submitted, having received
// received, and the same construction over the base.
func myRevisionViews(
	kind documentKind, id int64, status queries.RevisionsStatus, doc, base domain.AnyDocument,
	received domain.ElementKeySet, vis domain.EditorVisibility,
) (domain.Snapshot, domain.Snapshot, error) {
	var view, baseView domain.Snapshot
	var err error
	if status == queries.RevisionsStatusApproved {
		view, err = kind.view(id, doc, vis.SnapshotVisibility)
		if err == nil && base != nil {
			baseView, err = kind.view(id, base, vis.SnapshotVisibility)
		}
	} else {
		view, err = kind.authorView(id, doc, received, vis)
		if err == nil && base != nil {
			baseView, err = kind.projectedView(id, base, vis)
		}
	}

	return view, baseView, err
}
