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

var (
	_ datasources.DraftSaver     = (*Repository)(nil)
	_ datasources.DraftUpdater   = (*Repository)(nil)
	_ datasources.DraftGetter    = (*Repository)(nil)
	_ datasources.DraftDiscarder = (*Repository)(nil)
)

// txReader returns a catalogueReader reading in the write transaction tx.
func (r *Repository) txReader(tx *sql.Tx) *catalogueReader {
	return &catalogueReader{q: r.queries.WithTx(tx), run: txRunner(tx)}
}

// SaveDraft saves a draft, under a lock on the author's users row, which makes one draft per author and
// entity and serialises the draft count. A new draft of an existing entity has the request's base, or the current
// revision, and records the keys of what its author received: those of the pending revision it starts from, if
// its base is that revision's submitted base, and otherwise those of the base's projection.
func (r *Repository) SaveDraft(ctx context.Context, req datasources.SaveDraftRequest) (datasources.SavedDraft, error) {
	var saved datasources.SavedDraft
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		saved, err = r.saveDraft(ctx, r.txReader(tx), req)
		return err
	})
	if err != nil {
		return datasources.SavedDraft{}, databaseError(fmt.Sprintf("saving draft of %s", req.EntityType), err)
	}

	return saved, nil
}

func (r *Repository) saveDraft(
	ctx context.Context, c *catalogueReader, req datasources.SaveDraftRequest,
) (datasources.SavedDraft, error) {
	kind, err := kindFor(req.EntityType)
	if err != nil {
		return datasources.SavedDraft{}, err
	}
	if req.Document.EntityType() != req.EntityType || req.Entity != nil && req.Entity.Type != req.EntityType {
		return datasources.SavedDraft{}, datasources.ErrDocumentType
	}
	if _, err := c.q.LockUser(ctx, req.Actor.UserID); err != nil {
		return datasources.SavedDraft{}, fmt.Errorf("locking user: %w", lookupError("user", err))
	}
	pending := c.referencedPending(ctx, req.Actor)
	if c.err != nil {
		return datasources.SavedDraft{}, c.err
	}
	snapshot, err := encodeJSON(req.Document)
	if err != nil {
		return datasources.SavedDraft{}, err
	}

	draft := queries.InsertDraftParams{
		EntityType: queries.RevisionsEntityType(req.EntityType), AuthorUserID: nullString(&req.Actor.UserID),
		SnapshotJson: snapshot, Summary: req.Summary, CreatedAt: req.Now, UpdatedAt: nullTime(req.Now),
	}
	if req.Entity == nil {
		if req.BaseRevisionID != nil {
			return datasources.SavedDraft{}, baseProblem()
		}
		return c.insertDraft(ctx, req.Actor, draft, domain.ElementKeySet{}, pending)
	}

	return c.saveEntityDraft(ctx, req, kind, draft, pending)
}

// saveEntityDraft saves a draft of an existing entity, or of the author's own pending new entity: it replaces the
// author's draft of the entity, if there is one, or inserts draft on its base.
func (c *catalogueReader) saveEntityDraft(
	ctx context.Context, req datasources.SaveDraftRequest, kind documentKind, draft queries.InsertDraftParams,
	pending []pendingNewEntity,
) (datasources.SavedDraft, error) {
	target, err := c.editTarget(ctx, *req.Entity, c.pendingNewEntities(ctx, req.Actor.UserID, false))
	if err != nil {
		return datasources.SavedDraft{}, err
	}
	existing, err := c.q.GetUserDraftForEntity(ctx, queries.GetUserDraftForEntityParams{
		AuthorUserID: draft.AuthorUserID, EntityType: draft.EntityType, EntityID: nullableID(&target.entity.ID),
	})
	if err == nil {
		return c.replaceDraft(ctx, req, existing.ID, draft.SnapshotJson, pending)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return datasources.SavedDraft{}, fmt.Errorf("fetching draft: %w", err)
	}
	base, received, err := c.draftBase(ctx, req, kind, target, pending)
	if err != nil {
		return datasources.SavedDraft{}, err
	}
	draft.EntityID, draft.BaseRevisionID = nullableID(&target.entity.ID), nullableID(base)

	return c.insertDraft(ctx, req.Actor, draft, received, pending)
}

// insertDraft inserts a new draft, which received received, unless its author holds domain.MaxDrafts drafts.
func (c *catalogueReader) insertDraft(
	ctx context.Context, actor domain.Actor, draft queries.InsertDraftParams, received domain.ElementKeySet,
	pending []pendingNewEntity,
) (datasources.SavedDraft, error) {
	count, err := c.q.CountDrafts(ctx, draft.AuthorUserID)
	if err != nil {
		return datasources.SavedDraft{}, fmt.Errorf("counting drafts: %w", err)
	}
	if count >= domain.MaxDrafts {
		return datasources.SavedDraft{}, datasources.ErrDraftLimitReached
	}
	if draft.ReceivedKeysJson, err = encodeJSON(received); err != nil {
		return datasources.SavedDraft{}, err
	}
	inserted, err := c.q.InsertDraft(ctx, draft)
	if err != nil {
		return datasources.SavedDraft{}, fmt.Errorf("inserting draft: %w", err)
	}
	id, ok := positiveID(inserted)
	if !ok {
		return datasources.SavedDraft{}, fmt.Errorf("inserted draft ID [%d] out of range", inserted)
	}

	saved, err := c.draft(ctx, actor, id, pending)
	return datasources.SavedDraft{Draft: saved, Created: true}, err
}

// replaceDraft replaces the document and summary of the author's draft id of the entity, keeping its base.
func (c *catalogueReader) replaceDraft(
	ctx context.Context, req datasources.SaveDraftRequest, id uint64, snapshot []byte, pending []pendingNewEntity,
) (datasources.SavedDraft, error) {
	err := requireMatched(c.q.UpdateDraftDocument(ctx, queries.UpdateDraftDocumentParams{
		SnapshotJson: snapshot, Summary: req.Summary, UpdatedAt: nullTime(req.Now), ID: id,
	}))
	if err != nil {
		return datasources.SavedDraft{}, fmt.Errorf("replacing draft [%d]: %w", id, err)
	}
	draft, err := c.draft(ctx, req.Actor, id, pending)

	return datasources.SavedDraft{Draft: draft}, err
}

// draftBase returns the base of a new draft of target and the keys of what its author received of it. The
// request's base must be an approved revision of the entity. Without one, the base is that of the document the
// editor serves (docs/PLAN.md §5.1, "Drafts"): the submitted base of the author's pending revision of the entity if
// they have one, else the current revision.
func (c *catalogueReader) draftBase(
	ctx context.Context, req datasources.SaveDraftRequest, kind documentKind, target editTarget,
	pending []pendingNewEntity,
) (*int64, domain.ElementKeySet, error) {
	start, err := c.pendingStart(ctx, req.Actor, kind, target, pending)
	if err != nil {
		return nil, nil, err
	}
	base := req.BaseRevisionID
	if base == nil && start != nil {
		base = start.base
	}
	if base == nil {
		base = target.currentRevision
	}
	if base == nil {
		received := domain.ElementKeySet{}
		if start != nil {
			received = start.received
		}
		return nil, received, nil
	}
	if start != nil && start.base != nil && *start.base == *base {
		return base, start.received, nil
	}
	doc, err := c.approvedDocument(ctx, kind, target.entity, *base)
	if errors.Is(err, datasources.ErrNotFound) {
		return nil, nil, baseProblem()
	}
	if err != nil {
		return nil, nil, err
	}
	vis := c.editorVisibility(ctx, req.Actor, target.entity.ID, kind, pending, doc)
	_, received := kind.editable(target.entity.ID, doc, vis)

	return base, received, c.err
}

// baseProblem is the error for a draft base that is not an approved revision of the entity.
func baseProblem() error {
	return &domain.DocumentError{Problems: []domain.DocumentProblem{
		{Path: "/base_revision_id", Code: domain.ProblemUnknownReference},
	}}
}

// UpdateDraft replaces the document and summary of one of the actor's drafts, keeping its base.
func (r *Repository) UpdateDraft(ctx context.Context, req datasources.UpdateDraftRequest) (domain.Draft, error) {
	var draft domain.Draft
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		row, err := c.lockOwnRevision(ctx, req.Actor, req.ID, domain.RevisionActionSaveDraft)
		if err != nil {
			return err
		}
		if string(row.EntityType) != string(req.Document.EntityType()) {
			return datasources.ErrDocumentType
		}
		snapshot, err := encodeJSON(req.Document)
		if err != nil {
			return err
		}
		err = requireMatched(c.q.UpdateDraftDocument(ctx, queries.UpdateDraftDocumentParams{
			SnapshotJson: snapshot, Summary: req.Summary, UpdatedAt: nullTime(req.Now), ID: row.ID,
		}))
		if err != nil {
			return fmt.Errorf("updating draft: %w", err)
		}
		draft, err = c.draft(ctx, req.Actor, row.ID, c.referencedPending(ctx, req.Actor))
		return err
	})
	if err != nil {
		return domain.Draft{}, databaseError(fmt.Sprintf("updating draft [%d]", req.ID), err)
	}

	return draft, nil
}

// GetDraft returns one of the actor's drafts.
func (r *Repository) GetDraft(ctx context.Context, actor domain.Actor, id int64) (domain.Draft, error) {
	var draft domain.Draft
	err := r.readOnly(ctx, func(c *catalogueReader) error {
		row, err := c.ownRevision(ctx, actor, id)
		if err != nil {
			return err
		}
		if row.Status != queries.RevisionsStatusDraft {
			return domain.ErrNotDraft
		}
		draft, err = c.draft(ctx, actor, row.ID, c.referencedPending(ctx, actor))
		if err != nil {
			return err
		}
		return c.err
	})
	if err != nil {
		return domain.Draft{}, fmt.Errorf("getting draft [%d]: %w", id, err)
	}

	return draft, nil
}

// DiscardDraft deletes one of the actor's drafts.
func (r *Repository) DiscardDraft(ctx context.Context, actor domain.Actor, id int64) error {
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		row, err := c.lockOwnRevision(ctx, actor, id, domain.RevisionActionDiscardDraft)
		if err != nil {
			return err
		}
		return requireMatched(c.q.DeleteDraft(ctx, row.ID))
	})
	if err != nil {
		return databaseError(fmt.Sprintf("discarding draft [%d]", id), err)
	}

	return nil
}

// ownRevision returns the revision id if the actor is its author, and datasources.ErrNotFound otherwise, so that
// another user's revision is not revealed to exist.
func (c *catalogueReader) ownRevision(ctx context.Context, actor domain.Actor, id int64) (queries.GetRevisionRow,
	error,
) {
	if id < 1 {
		return queries.GetRevisionRow{}, datasources.ErrNotFound
	}
	row, err := c.q.GetRevision(ctx, uint64(id))
	if err != nil {
		return queries.GetRevisionRow{}, lookupError("revision", err)
	}
	if !row.AuthorUserID.Valid || row.AuthorUserID.String != actor.UserID {
		return queries.GetRevisionRow{}, datasources.ErrNotFound
	}

	return row, nil
}

// lockOwnRevision locks the revision id FOR UPDATE and checks the actor may take action on it: it returns
// datasources.ErrNotFound unless the actor is its author, and the transition's error otherwise.
func (c *catalogueReader) lockOwnRevision(
	ctx context.Context, actor domain.Actor, id int64, action domain.RevisionAction,
) (queries.LockRevisionsRow, error) {
	if id < 1 {
		return queries.LockRevisionsRow{}, datasources.ErrNotFound
	}
	rows, err := c.q.LockRevisions(ctx, []uint64{uint64(id)})
	if err != nil {
		return queries.LockRevisionsRow{}, fmt.Errorf("locking revision: %w", err)
	}
	if len(rows) == 0 {
		return queries.LockRevisionsRow{}, datasources.ErrNotFound
	}
	row := rows[0]
	if _, err := checkTransition(actor, row, action); err != nil {
		return queries.LockRevisionsRow{}, err
	}

	return row, nil
}

// checkTransition decides action on the locked revision row by actor (domain.CheckTransition). A revision the actor
// may not act on as its author is not found.
func checkTransition(actor domain.Actor, row queries.LockRevisionsRow, action domain.RevisionAction) (
	domain.RevisionStatus, error,
) {
	from := domain.RevisionStatus(row.Status)
	to, err := domain.CheckTransition(domain.TransitionInput{
		Role: actor.Role, IsAuthor: row.AuthorUserID.Valid && row.AuthorUserID.String == actor.UserID, From: &from,
		Action: action,
	})
	if errors.Is(err, domain.ErrNotRevisionAuthor) {
		return "", datasources.ErrNotFound
	}

	return to, err
}

// draft returns the draft id as its author sees it: with its references, and its preview, the author's view of it.
func (c *catalogueReader) draft(
	ctx context.Context, actor domain.Actor, id uint64, pending []pendingNewEntity,
) (domain.Draft, error) {
	row, err := c.q.GetRevision(ctx, id)
	if err != nil {
		return domain.Draft{}, fmt.Errorf("fetching draft: %w", err)
	}
	entityType := enumOf[domain.EntityType](c, row.EntityType)
	kind, doc, err := decodeRevision(entityType, row.SnapshotJson)
	if err != nil {
		return domain.Draft{}, fmt.Errorf("draft: %w", err)
	}
	received, err := decodeJSONColumn[domain.ElementKeySet]("received keys", row.ReceivedKeysJson)
	if err != nil {
		return domain.Draft{}, err
	}
	entityID, entity, err := c.revisionEntity(ctx, entityType, row.EntityID)
	if err != nil {
		return domain.Draft{}, err
	}

	vis := c.editorVisibility(ctx, actor, entityID, kind, pending, doc)
	preview, err := kind.authorView(entityID, doc, received, vis)
	if err != nil {
		return domain.Draft{}, fmt.Errorf("draft preview: %w", err)
	}
	var refs domain.SnapshotReferences
	kind.addReferences(entityID, doc, &refs)

	return domain.Draft{
		ID: c.signedID(row.ID), EntityType: entityType, Entity: entity, BaseRevisionID: int64Ptr(row.BaseRevisionID),
		Document: doc, Summary: row.Summary, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt.Time,
		References: c.editorReferences(ctx, actor, refs, pending), Preview: preview,
	}, c.err
}

// revisionEntity returns the ID of a revision's entity (0 for none) and the entity as its author addresses it, nil
// if it has none or its row is gone (a deleted placeholder).
func (c *catalogueReader) revisionEntity(
	ctx context.Context, entityType domain.EntityType, entityID sql.NullInt64,
) (int64, *domain.RevisionEntity, error) {
	entity, _, err := c.entityAndCurrent(ctx, entityType, entityID)
	if err != nil {
		return 0, nil, err
	}

	return entityID.Int64, entity, nil
}
