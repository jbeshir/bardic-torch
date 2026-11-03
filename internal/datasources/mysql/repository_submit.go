package mysql

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.RevisionSubmitter  = (*Repository)(nil)
	_ datasources.RevisionWithdrawer = (*Repository)(nil)
)

// errBaseNotApproved is a draft whose base is not an approved revision of its entity, which saving a draft never
// stores (R19): an invariant broken, not a caller's mistake.
var errBaseNotApproved = errors.New("draft base is not an approved revision of its entity")

// SubmitDraft submits one of the actor's drafts (D2, D5, D15). In one transaction, in the global lock order, it
// locks the author's users row (the pending cap); the draft and the author's pending revision of the entity it
// supersedes, by ascending ID; then the entity row FOR UPDATE and the rows of the entities the documents reference
// FOR SHARE. A draft already submitted is a replay, returned as it is. The submission is planned on the draft's own
// base, never merged with the current document (domain.PlanSubmit), stored as pending with the author's document
// kept beside it, and supersedes the author's pending revision of the entity. A new entity gets its placeholder.
// An interactive moderator's submission is approved at once, in the same transaction (D3).
func (r *Repository) SubmitDraft(ctx context.Context, req datasources.SubmitRequest) (datasources.SubmitResult, error) {
	var result datasources.SubmitResult
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		result, err = r.submitDraft(ctx, tx, req)
		return err
	})
	if err != nil {
		return datasources.SubmitResult{}, withoutServerMessage(fmt.Sprintf("submitting draft [%d]", req.DraftID), err)
	}

	return result, nil
}

func (r *Repository) submitDraft(
	ctx context.Context, tx *sql.Tx, req datasources.SubmitRequest,
) (datasources.SubmitResult, error) {
	c := r.txReader(tx)
	s, err := c.lockSubmission(ctx, req.Actor, req.DraftID)
	if err != nil {
		return datasources.SubmitResult{}, err
	}
	if s.result.Replayed {
		return s.result, nil
	}
	docs, err := c.loadSubmission(ctx, s.draft)
	if err != nil {
		return datasources.SubmitResult{}, err
	}
	if s.result.Status == domain.RevisionStatusApproved {
		return s.result, c.submitApproved(ctx, req, s, docs)
	}

	count, err := c.q.CountPendingRevisions(ctx, queries.CountPendingRevisionsParams{
		AuthorUserID: s.draft.AuthorUserID, ExcludedID: s.superseded,
	})
	if err != nil {
		return datasources.SubmitResult{}, fmt.Errorf("counting pending revisions: %w", err)
	}
	if count >= domain.MaxOpenPendingRevisions {
		return datasources.SubmitResult{}, datasources.ErrPendingLimitReached
	}

	doc, flags, err := c.planSubmission(ctx, req, docs, s.draft)
	if err != nil {
		return datasources.SubmitResult{}, err
	}
	if !s.draft.EntityID.Valid && len(req.Duplicates) > 0 {
		flags = append(flags, domain.Flag{Code: domain.FlagPossibleDuplicate})
	}
	_, err = c.writeSubmission(ctx, req, s, docs.entity, doc, flags)

	return s.result, err
}

// submitApproved submits an interactive moderator's draft, which is approved at once (D3): restored like any
// submission (only the preferences were withheld), stored as pending, then approved inline by the approval writer,
// the moderator its reviewer, in the same transaction, so that a refused approval leaves the draft a draft. The
// pending cap does not apply: the revision does not stay pending.
func (c *catalogueReader) submitApproved(
	ctx context.Context, req datasources.SubmitRequest, s submission, docs submissionDocuments,
) error {
	doc, flags, err := c.planSubmission(ctx, req, docs, s.draft)
	if err != nil {
		return err
	}
	entity, err := c.writeSubmission(ctx, req, s, docs.entity, doc, flags)
	if err != nil {
		return err
	}

	return c.approve(ctx, approvalInput{
		revisionID: s.draft.ID, authorID: req.Actor.UserID, author: domain.RoleModerator, entity: entity,
		kind: docs.kind, doc: doc, baseRevisionID: int64Ptr(s.draft.BaseRevisionID), summary: s.draft.Summary,
		storedFlags: flags, reviewer: req.Actor.UserID,
		options: domain.ApprovalOptions{AcknowledgedFlags: req.AcknowledgedFlags}, duplicates: req.Duplicates,
		now: req.Now,
	})
}

// submission is a draft being submitted, locked: its row, the ID of the pending revision it supersedes (0 if
// none) and the result the submission gives.
type submission struct {
	draft      queries.GetRevisionRow
	superseded uint64
	result     datasources.SubmitResult
}

// lockSubmission takes the submission's first locks (D15): the author's users row, then the draft and the author's
// pending revision of its entity, if any, which the submission supersedes, by ascending ID. That pending revision is
// found by an unlocked read and checked again once locked. The transition is checked on the locked draft: one
// already submitted is a replay.
func (c *catalogueReader) lockSubmission(ctx context.Context, actor domain.Actor, draftID int64) (submission, error) {
	if _, err := c.q.LockUser(ctx, actor.UserID); err != nil {
		return submission{}, fmt.Errorf("locking user: %w", lookupError("user", err))
	}
	draft, err := c.ownRevision(ctx, actor, draftID)
	if err != nil {
		return submission{}, err
	}
	candidate, err := c.supersededCandidate(ctx, draft)
	if err != nil {
		return submission{}, err
	}
	ids := []uint64{draft.ID}
	if candidate != 0 {
		ids = append(ids, candidate)
	}
	slices.Sort(ids)
	rows, err := c.q.LockRevisions(ctx, ids)
	if err != nil {
		return submission{}, fmt.Errorf("locking revisions: %w", err)
	}

	locked, superseded := lockedSubmission(rows, draft)
	s := submission{draft: draft, superseded: superseded}
	if locked == nil {
		return submission{}, datasources.ErrNotFound
	}
	to, err := checkTransition(actor, *locked, domain.RevisionActionSubmit)
	if err != nil {
		return submission{}, err
	}
	s.result = datasources.SubmitResult{RevisionID: c.signedID(locked.ID), Status: to,
		Replayed: locked.Status != queries.RevisionsStatusDraft}

	return s, nil
}

// lockedSubmission returns, of the locked rows, the draft's (nil if it is gone) and the ID of the pending revision it
// supersedes: the author's, of the same entity, still pending (0 if none).
func lockedSubmission(
	rows []queries.LockRevisionsRow, draft queries.GetRevisionRow,
) (*queries.LockRevisionsRow, uint64) {
	var locked *queries.LockRevisionsRow
	var superseded uint64
	for _, row := range rows {
		if row.ID == draft.ID {
			locked = &row
		} else if row.Status == queries.RevisionsStatusPending && row.AuthorUserID == draft.AuthorUserID &&
			row.EntityType == draft.EntityType && row.EntityID == draft.EntityID {
			superseded = row.ID
		}
	}

	return locked, superseded
}

// supersededCandidate returns the ID of the author's pending revision of the draft's entity, read without a lock,
// or 0 if there is none.
func (c *catalogueReader) supersededCandidate(ctx context.Context, draft queries.GetRevisionRow) (uint64, error) {
	if !draft.EntityID.Valid || draft.Status != queries.RevisionsStatusDraft {
		return 0, nil
	}
	pending, err := c.q.GetUserPendingForEntity(ctx, queries.GetUserPendingForEntityParams{
		AuthorUserID: draft.AuthorUserID, EntityType: draft.EntityType, EntityID: draft.EntityID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("fetching superseded revision: %w", err)
	}

	return pending.ID, nil
}

// submissionDocuments are the documents of a submission: the submitted document and the full document of the
// draft's base, nil for a new entity, of the draft's entity, whose ID is 0 for a new entity.
type submissionDocuments struct {
	kind      documentKind
	entity    domain.EntityRef
	submitted domain.AnyDocument
	base      domain.AnyDocument
}

// all returns the documents there are.
func (d submissionDocuments) all() []domain.AnyDocument {
	if d.base == nil {
		return []domain.AnyDocument{d.submitted}
	}

	return []domain.AnyDocument{d.submitted, d.base}
}

// references returns what the documents reference.
func (d submissionDocuments) references() domain.SnapshotReferences {
	var refs domain.SnapshotReferences
	for _, doc := range d.all() {
		d.kind.addReferences(d.entity.ID, doc, &refs)
	}

	return refs
}

// loadSubmission decodes the draft's document and loads its base's, then takes the submission's entity locks
// (D15): the draft's entity row FOR UPDATE, which must still exist, and the rows of the entities either document
// references FOR SHARE.
func (c *catalogueReader) loadSubmission(ctx context.Context, draft queries.GetRevisionRow) (
	submissionDocuments, error,
) {
	entityType := enumOf[domain.EntityType](c, draft.EntityType)
	kind, err := kindFor(entityType)
	if err != nil {
		return submissionDocuments{}, err
	}
	docs := submissionDocuments{kind: kind, entity: domain.EntityRef{Type: entityType, ID: draft.EntityID.Int64}}
	if docs.submitted, err = kind.decode(draft.SnapshotJson); err != nil {
		return submissionDocuments{}, fmt.Errorf("draft snapshot: %w", err)
	}
	if docs.base, err = c.draftBaseDocument(ctx, kind, docs.entity, draft.BaseRevisionID); err != nil {
		return submissionDocuments{}, err
	}
	if _, err := c.lockOwnEntity(ctx, docs.entity, docs.references()); err != nil {
		return submissionDocuments{}, err
	}

	return docs, nil
}

// writeSubmission writes a planned submission: a new entity's placeholder, the superseded revision withdrawn, and
// the draft stored as pending. It returns the submission's entity, with its placeholder's ID for a new one.
func (c *catalogueReader) writeSubmission(
	ctx context.Context, req datasources.SubmitRequest, s submission, entity domain.EntityRef,
	doc domain.AnyDocument, flags []domain.Flag,
) (domain.EntityRef, error) {
	if !s.draft.EntityID.Valid {
		id, err := c.insertPlaceholder(ctx, doc)
		if err != nil {
			return domain.EntityRef{}, err
		}
		entity.ID = id
	}
	if s.superseded != 0 {
		if err := requireMatched(c.q.WithdrawRevision(ctx, s.superseded)); err != nil {
			return domain.EntityRef{}, fmt.Errorf("superseding revision [%d]: %w", s.superseded, err)
		}
	}

	return entity, c.storeSubmission(ctx, req, s.draft, entity, doc, flags)
}

// draftBaseDocument returns the document of a draft's base, nil for none (a new entity). The base must be an
// approved revision of the draft's entity (R19).
func (c *catalogueReader) draftBaseDocument(
	ctx context.Context, kind documentKind, entity domain.EntityRef, baseID sql.NullInt64,
) (domain.AnyDocument, error) {
	if !baseID.Valid {
		return nil, nil
	}
	doc, err := c.approvedDocument(ctx, kind, domain.RevisionEntity{Type: entity.Type, ID: entity.ID}, baseID.Int64)
	if errors.Is(err, datasources.ErrNotFound) {
		return nil, fmt.Errorf("%w [%d]", errBaseNotApproved, baseID.Int64)
	}

	return doc, err
}

// planSubmission loads what planning a contributor's submission needs, under the submission's locks, and plans it
// (domain.PlanSubmit): the full document to store and its flags, or a *domain.DocumentError.
func (c *catalogueReader) planSubmission(
	ctx context.Context, req datasources.SubmitRequest, docs submissionDocuments, draft queries.GetRevisionRow,
) (domain.AnyDocument, []domain.Flag, error) {
	received, err := decodeReceivedKeys(draft.ReceivedKeysJson)
	if err != nil {
		return nil, nil, err
	}
	pending := c.pendingNewEntities(ctx, req.Actor.UserID, false)
	vis := c.editorVisibility(ctx, req.Actor, docs.entity.ID, docs.kind, pending, docs.all()...)
	index := c.referenceIndex(ctx, docs.references(), vis.OwnPending, docs.submitted)
	hidden := fetchAll(ctx, c, "hidden aliases", c.q.ListHiddenAliasNames)
	if c.err != nil {
		return nil, nil, c.err
	}

	return docs.kind.planSubmit(submitState{
		id: docs.entity.ID, submitted: docs.submitted, base: docs.base, received: received, vis: vis, refs: index,
		hiddenAliases: hidden, summary: draft.Summary, today: dateOf(req.Now),
	})
}

// dateOf is the UTC date of t.
func dateOf(t time.Time) domain.Date {
	year, month, day := t.UTC().Date()

	return domain.Date{Time: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// referenceIndex loads what validation knows of the entities refs references (domain.ReferenceIndex), own being the
// author's pending new terms and persons. For a term document it holds every term of its facet, and for collections
// their ancestors.
func (c *catalogueReader) referenceIndex(
	ctx context.Context, refs domain.SnapshotReferences, own domain.OwnPendingRefs, doc domain.AnyDocument,
) domain.ReferenceIndex {
	index := domain.ReferenceIndex{
		Pieces: map[int64]domain.ReferencedEntity{}, Persons: map[int64]domain.ReferencedPerson{},
		Terms: map[int64]domain.ReferencedTerm{}, Collections: map[int64]domain.ReferencedCollection{},
		Events: map[int64]domain.ReferencedEntity{},
	}
	if ids := unsignedIDs(refs.PieceIDs); len(ids) > 0 {
		for _, row := range fetch(ctx, c, "referenced pieces", c.q.ListPieceReferences, ids) {
			index.Pieces[c.signedID(row.ID)] = domain.ReferencedEntity{Public: row.IsPublic}
		}
	}
	if ids := unsignedIDs(refs.PersonIDs); len(ids) > 0 {
		for _, row := range fetch(ctx, c, "referenced persons", c.q.ListPersonReferences, ids) {
			id := c.signedID(row.ID)
			_, ownPending := own.Persons[id]
			index.Persons[id] = domain.ReferencedPerson{
				ReferencedEntity: domain.ReferencedEntity{Public: row.IsPublic, OwnPending: ownPending},
				PrefersNoPage:    row.Status == queries.PersonsStatusPrefersNoPage,
			}
		}
	}
	addTerm := func(id uint64, facet queries.TermsFacet, parent sql.NullInt64, name string, public bool) {
		signed := c.signedID(id)
		_, ownPending := own.Terms[signed]
		term := domain.ReferencedTerm{
			ReferencedEntity: domain.ReferencedEntity{Public: public, OwnPending: ownPending},
			Facet:            enumOf[domain.Facet](c, facet), ParentID: int64Ptr(parent),
		}
		if public {
			term.Name = name
		}
		index.Terms[signed] = term
	}
	if ids := unsignedIDs(refs.TermIDs); len(ids) > 0 {
		for _, row := range fetch(ctx, c, "referenced terms", c.q.ListTermReferences, ids) {
			addTerm(row.ID, row.Facet, row.ParentID, row.Name, row.IsPublic)
		}
	}
	if d, ok := doc.(domain.TermDocument); ok {
		for _, row := range fetch(ctx, c, "facet terms", c.q.ListFacetTermReferences, queries.TermsFacet(d.Facet)) {
			addTerm(row.ID, row.Facet, row.ParentID, row.Name, row.IsPublic)
		}
	}
	c.collectionReferences(ctx, refs.CollectionIDs, index.Collections)
	if ids := unsignedIDs(refs.EventIDs); len(ids) > 0 {
		for _, row := range fetch(ctx, c, "referenced events", c.q.ListEventReferences, ids) {
			index.Events[c.signedID(row.ID)] = domain.ReferencedEntity{Public: row.IsPublic}
		}
	}

	return index
}

// collectionReferences loads the referenced collections ids and their ancestors into collections.
func (c *catalogueReader) collectionReferences(
	ctx context.Context, ids []int64, collections map[int64]domain.ReferencedCollection,
) {
	for next := unsignedIDs(ids); len(next) > 0 && c.err == nil; {
		var parents []int64
		for _, row := range fetch(ctx, c, "referenced collections", c.q.ListCollectionReferences, next) {
			id := c.signedID(row.ID)
			collections[id] = domain.ReferencedCollection{
				ReferencedEntity: domain.ReferencedEntity{Public: row.IsPublic}, ParentID: int64Ptr(row.ParentID),
			}
			if _, seen := collections[row.ParentID.Int64]; row.ParentID.Valid && !seen {
				parents = append(parents, row.ParentID.Int64)
			}
		}
		next = unsignedIDs(parents)
	}
}

// insertPlaceholder inserts the placeholder of the pending new entity doc describes (D14), holding no content:
// a random slug, empty names, and for a term its depth-0 closure row, without which public_terms would hold it.
func (c *catalogueReader) insertPlaceholder(ctx context.Context, doc domain.AnyDocument) (int64, error) {
	slug, err := placeholderSlug(doc.EntityType())
	if err != nil {
		return 0, err
	}
	var id int64
	switch d := doc.(type) {
	case domain.PieceDocument:
		id, err = c.q.InsertPiecePlaceholder(ctx, queries.InsertPiecePlaceholderParams{
			Slug: slug, Kind: queries.PiecesKind(d.Kind)})
	case domain.PersonDocument:
		id, err = c.q.InsertPersonPlaceholder(ctx, queries.InsertPersonPlaceholderParams{
			Slug: slug, Kind: queries.PersonsKind(d.Kind)})
	case domain.TermDocument:
		id, err = c.q.InsertTermPlaceholder(ctx, queries.InsertTermPlaceholderParams{
			Facet: queries.TermsFacet(d.Facet), Slug: slug})
		if term, ok := positiveID(id); err == nil && ok {
			err = c.q.InsertTermAncestor(ctx, queries.InsertTermAncestorParams{
				AncestorID: term, DescendantID: term, Depth: 0})
		}
	case domain.CollectionDocument:
		id, err = c.q.InsertCollectionPlaceholder(ctx, queries.CollectionsKind(d.Kind))
	case domain.EventDocument:
		id, err = c.q.InsertEventPlaceholder(ctx, queries.InsertEventPlaceholderParams{
			Slug: slug, Kind: queries.EventsKind(d.Kind)})
	}
	if err != nil {
		return 0, fmt.Errorf("inserting %s placeholder: %w", doc.EntityType(), err)
	}

	return id, nil
}

// placeholderSlug returns a new placeholder slug for a pending new entity of entityType (D14):
// pending-<type>-<32 hex digits>, from crypto/rand.
func placeholderSlug(entityType domain.EntityType) (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generating placeholder slug: %w", err)
	}

	return "pending-" + string(entityType) + "-" + hex.EncodeToString(random), nil
}

// storeSubmission stores the draft as pending: the full document doc and its flags, the author's document as
// submitted, the time, and the draft's base as both its base and its submitted base (D5).
func (c *catalogueReader) storeSubmission(
	ctx context.Context, req datasources.SubmitRequest, draft queries.GetRevisionRow, entity domain.EntityRef,
	doc domain.AnyDocument, flags []domain.Flag,
) error {
	snapshot, err := encodeJSON(doc)
	if err != nil {
		return err
	}
	if flags == nil {
		flags = []domain.Flag{}
	}
	flagsJSON, err := encodeJSON(flags)
	if err != nil {
		return err
	}
	err = requireMatched(c.q.SubmitDraft(ctx, queries.SubmitDraftParams{
		EntityID: nullableID(&entity.ID), SnapshotJson: snapshot, SubmittedSnapshotJson: draft.SnapshotJson,
		FlagsJson: flagsJSON, SubmittedAt: nullTime(req.Now), BaseRevisionID: draft.BaseRevisionID, ID: draft.ID,
	}))
	if err != nil {
		return fmt.Errorf("storing submission: %w", err)
	}

	return nil
}

// WithdrawRevision withdraws one of the actor's pending revisions (D2). It locks the revision, then its entity's
// row (D15), and deletes a pending new entity's placeholder if no other pending revision has it (D14); the revision
// row stays.
func (r *Repository) WithdrawRevision(ctx context.Context, actor domain.Actor, id int64) error {
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		row, err := c.lockOwnRevision(ctx, actor, id, domain.RevisionActionWithdraw)
		if err != nil {
			return err
		}
		return c.leavePending(ctx, row, func() error {
			return requireMatched(c.q.WithdrawRevision(ctx, row.ID))
		})
	})
	if err != nil {
		return withoutServerMessage(fmt.Sprintf("withdrawing revision [%d]", id), err)
	}

	return nil
}

// deleteUnusedPlaceholder deletes the placeholder of the pending new entity, which its revision revisionID has left
// pending without being approved, unless another pending revision has it (D14).
func (c *catalogueReader) deleteUnusedPlaceholder(
	ctx context.Context, entity domain.EntityRef, revisionID uint64,
) error {
	others, err := c.q.CountOtherPendingRevisionsOfEntity(ctx, queries.CountOtherPendingRevisionsOfEntityParams{
		EntityType: queries.RevisionsEntityType(entity.Type), EntityID: nullableID(&entity.ID), ExcludedID: revisionID,
	})
	if err != nil {
		return fmt.Errorf("counting pending revisions of %s [%d]: %w", entity.Type, entity.ID, err)
	}
	if others > 0 {
		return nil
	}

	return c.deletePlaceholder(ctx, entity)
}

// leavePending ends the locked pending revision row without approving it, by write (D14): it locks the revision's
// entity row, writes, and deletes a pending new entity's placeholder if no other pending revision has it.
func (c *catalogueReader) leavePending(ctx context.Context, row queries.LockRevisionsRow, write func() error) error {
	entity := domain.EntityRef{Type: enumOf[domain.EntityType](c, row.EntityType), ID: row.EntityID.Int64}
	entityRow, err := c.entityRow(ctx, entity.Type, entity.ID, true)
	gone := errors.Is(err, datasources.ErrNotFound)
	if err != nil && !gone {
		return err
	}
	if err := write(); err != nil {
		return fmt.Errorf("ending pending revision: %w", err)
	}
	if gone || entityRow.currentRevision != nil {
		return c.err
	}

	return c.deleteUnusedPlaceholder(ctx, entity, row.ID)
}
