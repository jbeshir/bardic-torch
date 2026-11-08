package mysql

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var (
	_ datasources.RevisionRejecter = (*Repository)(nil)
	_ datasources.RevisionRebaser  = (*Repository)(nil)
	_ datasources.RevisionReverter = (*Repository)(nil)
)

// RejectRevision rejects a pending revision: it locks the revision, checks the review note, then locks
// its entity's row, rejects it and deletes a pending new entity's placeholder if no other pending revision has it
// .
func (r *Repository) RejectRevision(ctx context.Context, req datasources.RejectRequest) error {
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		return r.txReader(tx).rejectRevision(ctx, req)
	})
	if err != nil {
		return databaseError(fmt.Sprintf("rejecting revision [%d]", req.RevisionID), err)
	}

	return nil
}

// rejectRevision is RejectRevision's transaction.
func (c *catalogueReader) rejectRevision(ctx context.Context, req datasources.RejectRequest) error {
	row, err := c.lockModeratedRevision(ctx, req.Actor, req.RevisionID, domain.RevisionActionReject)
	if err != nil {
		return err
	}
	hidden := fetchAll(ctx, c, "hidden aliases", c.q.ListHiddenAliasNames)
	if c.err != nil {
		return c.err
	}
	if err := domain.CheckReviewNote(req.ReviewNote, domain.NewNameMatcher(hidden, nil),
		req.AcknowledgedFlags); err != nil {
		return err
	}

	return c.leavePending(ctx, row, func() error {
		return requireMatched(c.q.RejectRevisionRow(ctx, queries.RejectRevisionRowParams{
			ReviewerUserID: sql.NullString{String: req.Actor.UserID, Valid: true}, ReviewedAt: nullTime(req.Now),
			ReviewNote: optionalText(req.ReviewNote), ID: row.ID,
		}))
	})
}

// RebaseRevision rebases a pending revision onto its entity's current revision (docs/PLAN.md §5.1): under the
// revision's lock and its entity row's, the moderator's resolved document must be against the current revision, is
// validated against the current document with the revision author's role (domain.PlanRebase), and replaces the
// revision's document, which stays pending. Its stored flags lose those only the submission could find
// (domain.FlagsAfterRebase); what its author submitted, and the base they edited from, are kept.
func (r *Repository) RebaseRevision(ctx context.Context, req datasources.RebaseRequest) error {
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		return r.txReader(tx).rebaseRevision(ctx, req)
	})
	if err != nil {
		return databaseError(fmt.Sprintf("rebasing revision [%d]", req.RevisionID), err)
	}

	return nil
}

// rebaseRevision is RebaseRevision's transaction.
func (c *catalogueReader) rebaseRevision(ctx context.Context, req datasources.RebaseRequest) error {
	row, err := c.lockModeratedRevision(ctx, req.Actor, req.RevisionID, domain.RevisionActionRebase)
	if err != nil {
		return err
	}
	in, err := c.pendingApproval(ctx, row.ID)
	if err != nil {
		return err
	}
	if req.Document.EntityType() != in.entity.Type {
		return datasources.ErrDocumentType
	}

	return c.rebase(ctx, req, in)
}

func (c *catalogueReader) rebase(ctx context.Context, req datasources.RebaseRequest, in approvalInput) error {
	peeked, err := c.peekCurrent(ctx, in.kind, in.entity)
	if err != nil {
		return err
	}
	locks := referencesOf(in.kind, in.entity.ID, req.Document)
	peeked.references(in.kind, in.entity.ID, &locks)
	own, err := c.lockOwnEntity(ctx, in.entity, locks, nil)
	if err != nil {
		return err
	}
	if !domain.EqualPointers(own.currentRevision, req.CurrentRevisionID) {
		return datasources.ErrStale
	}
	locked, err := c.lockedCurrent(ctx, in.kind, in.entity, own, peeked)
	if err != nil {
		return err
	}
	current := locked.doc
	summary := in.summary
	if req.Summary != nil {
		summary = *req.Summary
	}
	authorPending := ownPendingRefs(c.pendingNewEntities(ctx, in.authorID, false))
	vis := c.editorVisibility(ctx, req.Actor, in.entity.ID, in.kind, nil, req.Document, in.doc)
	refs := referencesOf(in.kind, in.entity.ID, req.Document)
	if current != nil {
		in.kind.addReferences(in.entity.ID, current, &refs)
	}
	index := c.referenceIndex(ctx, refs, authorPending, req.Document)
	if c.err != nil {
		return c.err
	}
	doc, err := in.kind.planRebase(rebaseState{id: in.entity.ID, resolved: req.Document, sent: req.Sent,
		revision: in.doc, current: current,
		author: in.author, vis: vis, refs: index, summary: summary, today: dateOf(req.Now)})
	if err != nil {
		return err
	}
	snapshot, err := encodeJSON(doc)
	if err != nil {
		return err
	}
	flags, err := encodeJSON(domain.FlagsAfterRebase(in.storedFlags))
	if err != nil {
		return err
	}
	err = requireMatched(c.q.RebaseRevisionRow(ctx, queries.RebaseRevisionRowParams{SnapshotJson: snapshot,
		BaseRevisionID: nullableID(own.currentRevision), Summary: summary, FlagsJson: flags,
		RebasedByUserID: sql.NullString{String: req.Actor.UserID, Valid: true}, RebasedAt: nullTime(req.Now),
		ID: in.revisionID}))
	if err != nil {
		return fmt.Errorf("storing rebase: %w", err)
	}

	return nil
}

// RevertTo reverts an entity to one of its approved revisions, in the global lock order: the actor's users
// row (a contributor's pending cap), their pending revision of the entity, which the revert supersedes (found by an
// unlocked read and checked again once locked), then the entity row FOR UPDATE, whose current revision the revert
// is based on, and the rows the documents reference FOR SHARE. A contributor's revert is stored pending; an
// interactive moderator's is approved at once by the approval writer.
func (r *Repository) RevertTo(ctx context.Context, req datasources.RevertRequest) (datasources.SubmitResult, error) {
	var result datasources.SubmitResult
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		result, err = r.txReader(tx).revert(ctx, req)
		return err
	})
	if err != nil {
		return datasources.SubmitResult{}, databaseError(fmt.Sprintf("reverting to revision [%d]",
			req.RevisionID), err)
	}

	return result, nil
}

// revertTarget is a revert's target revision, of a public entity, the entity's locked current revision and document,
// and the actor's pending revision of the entity the revert supersedes (0 if none), with its document and base.
type revertTarget struct {
	row            queries.GetRevisionRow
	entity         domain.EntityRef
	kind           documentKind
	doc            domain.AnyDocument
	current        domain.AnyDocument
	currentID      int64
	superseded     uint64
	supersededJSON []byte
	supersededBase *int64
}

func (c *catalogueReader) revert(ctx context.Context, req datasources.RevertRequest) (datasources.SubmitResult, error) {
	status, err := c.revertStatus(req)
	if err != nil {
		return datasources.SubmitResult{}, err
	}
	t, err := c.lockRevert(ctx, req, status)
	if err != nil {
		return datasources.SubmitResult{}, err
	}
	pending := c.pendingNewEntities(ctx, req.Actor.UserID, false)
	vis := c.editorVisibility(ctx, req.Actor, t.entity.ID, t.kind, pending, t.doc, t.current)
	refs := referencesOf(t.kind, t.entity.ID, t.doc)
	t.kind.addReferences(t.entity.ID, t.current, &refs)
	index := c.referenceIndex(ctx, refs, vis.OwnPending, t.doc)
	hidden := fetchAll(ctx, c, "hidden aliases", c.q.ListHiddenAliasNames)
	if c.err != nil {
		return datasources.SubmitResult{}, c.err
	}
	plan, err := t.kind.planRevert(revertState{id: t.entity.ID, target: t.doc, current: t.current, vis: vis,
		refs: index, hiddenAliases: hidden, summary: req.Summary, today: dateOf(req.Now)})
	if err != nil {
		return datasources.SubmitResult{}, err
	}
	if replay, ok, err := c.revertReplay(ctx, req, t, plan.document, status); ok || err != nil {
		return replay, err
	}
	if err := c.supersedeForRevert(ctx, req.Actor, t, status); err != nil {
		return datasources.SubmitResult{}, err
	}
	id, err := c.insertRevert(ctx, req, t, plan)
	if err != nil {
		return datasources.SubmitResult{}, err
	}
	result := datasources.SubmitResult{RevisionID: c.signedID(id), Status: status}
	if status == domain.RevisionStatusPending {
		return result, c.err
	}

	return result, c.approve(ctx, approvalInput{
		revisionID: id, authorID: req.Actor.UserID, author: domain.RoleModerator, entity: t.entity, kind: t.kind,
		doc: plan.document, baseRevisionID: &t.currentID, summary: req.Summary,
		fallbackSummary: revertSummary(req, t), reviewer: req.Actor.UserID,
		options: domain.ApprovalOptions{AcknowledgedFlags: req.AcknowledgedFlags}, now: req.Now,
	})
}

// supersedeForRevert checks a pending revert against the actor's pending cap, not counting the pending revision of
// the entity it supersedes, and withdraws that revision.
func (c *catalogueReader) supersedeForRevert(
	ctx context.Context, actor domain.Actor, t revertTarget, status domain.RevisionStatus,
) error {
	if status != domain.RevisionStatusPending {
		return nil
	}
	if err := c.checkPendingCap(ctx, sql.NullString{String: actor.UserID, Valid: true}, t.superseded); err != nil {
		return err
	}

	return c.withdrawSuperseded(ctx, t.superseded)
}

// revertStatus is the status of the revision a revert by the request's actor creates: pending, or approved for an
// interactive moderator (domain.CheckTransition on the approved target).
func (c *catalogueReader) revertStatus(req datasources.RevertRequest) (domain.RevisionStatus, error) {
	approved := domain.RevisionStatusApproved

	return domain.CheckTransition(domain.TransitionInput{Role: req.Actor.Role, From: &approved,
		Action: domain.RevisionActionRevert})
}

// lockRevert reads the revert's target, which must be an approved revision of a public entity, and takes the
// revert's locks (docs/PLAN.md §5.1 "Revert"): for a pending revert the actor's users row and their pending
// revision of the entity, then in one pass the entity row and what the target and the current document reference,
// the current document being read before the lock and checked under it (lockedCurrent). A revert approved at once
// (by an interactive moderator) of a term also locks the terms of its facet, first among the terms, as its approval
// does. The target being the current revision is datasources.ErrAlreadyCurrent.
func (c *catalogueReader) lockRevert(
	ctx context.Context, req datasources.RevertRequest, status domain.RevisionStatus,
) (revertTarget, error) {
	t, err := c.revertTargetOf(ctx, req.RevisionID)
	if err != nil {
		return revertTarget{}, err
	}
	if status == domain.RevisionStatusPending {
		if err := c.lockSuperseded(ctx, req.Actor, &t); err != nil {
			return revertTarget{}, err
		}
	}
	peeked, err := c.peekCurrent(ctx, t.kind, t.entity)
	if err != nil {
		return revertTarget{}, err
	}
	refs := referencesOf(t.kind, t.entity.ID, t.doc)
	peeked.references(t.kind, t.entity.ID, &refs)
	var facets []domain.Facet
	if status == domain.RevisionStatusApproved {
		facets = rebuiltFacets(t.doc)
	}
	own, err := c.lockOwnEntity(ctx, t.entity, refs, facets)
	if errors.Is(err, datasources.ErrEntityGone) || err == nil && own.currentRevision == nil {
		return revertTarget{}, datasources.ErrNotFound
	}
	if err != nil {
		return revertTarget{}, err
	}
	t.currentID = *own.currentRevision
	if t.currentID == c.signedID(t.row.ID) {
		return revertTarget{}, datasources.ErrAlreadyCurrent
	}
	current, err := c.lockedCurrent(ctx, t.kind, t.entity, own, peeked)
	if err != nil {
		return revertTarget{}, err
	}
	t.current = current.doc

	return t, c.err
}

// revertTargetOf reads the revision id a revert is to, which must be an approved revision: approved revisions never
// change, so it needs no lock.
func (c *catalogueReader) revertTargetOf(ctx context.Context, id int64) (revertTarget, error) {
	key, ok := positiveID(id)
	if !ok {
		return revertTarget{}, datasources.ErrNotFound
	}
	row, err := c.q.GetRevision(ctx, key)
	if err != nil {
		return revertTarget{}, lookupError("revision", err)
	}
	if row.Status != queries.RevisionsStatusApproved || !row.EntityID.Valid {
		return revertTarget{}, datasources.ErrNotFound
	}
	t := revertTarget{row: row, entity: domain.EntityRef{Type: enumOf[domain.EntityType](c, row.EntityType),
		ID: row.EntityID.Int64}}
	if t.kind, t.doc, err = decodeRevision(t.entity.Type, row.SnapshotJson); err != nil {
		return revertTarget{}, fmt.Errorf("revision [%d]: %w", id, err)
	}

	return t, c.err
}

// lockSuperseded locks the actor's users row, then their pending revision of the target's entity, if any, which a
// pending revert supersedes: found by an unlocked read and checked again once locked.
func (c *catalogueReader) lockSuperseded(ctx context.Context, actor domain.Actor, t *revertTarget) error {
	if _, err := c.q.LockUser(ctx, actor.UserID); err != nil {
		return fmt.Errorf("locking user: %w", lookupError("user", err))
	}
	pending, found, err := c.userPendingFor(ctx, actor.UserID, t.row.EntityType, t.row.EntityID)
	if !found || err != nil {
		return err
	}
	rows, err := c.q.LockRevisions(ctx, []uint64{pending.ID})
	if err != nil {
		return fmt.Errorf("locking revisions: %w", err)
	}
	if len(rows) == 1 && rows[0].Status == queries.RevisionsStatusPending {
		locked, err := c.q.GetRevision(ctx, pending.ID)
		if err != nil {
			return fmt.Errorf("fetching superseded revision: %w", err)
		}
		t.superseded, t.supersededJSON = pending.ID, locked.SnapshotJson
		if locked.BaseRevisionID.Valid {
			t.supersededBase = &locked.BaseRevisionID.Int64
		}
	}

	return nil
}

// revertReplay returns the revision a replayed revert already created, and whether the revert is one: for a
// pending revert, the actor's pending revision of the entity holding the same document on the same base; for an
// approved one, the current revision holding the same document, if the actor authored it.
func (c *catalogueReader) revertReplay(
	ctx context.Context, req datasources.RevertRequest, t revertTarget, doc domain.AnyDocument,
	status domain.RevisionStatus,
) (datasources.SubmitResult, bool, error) {
	if status == domain.RevisionStatusPending {
		if t.superseded == 0 || t.supersededBase == nil || *t.supersededBase != t.currentID {
			return datasources.SubmitResult{}, false, nil
		}
		same, err := sameDocument(t.kind, t.supersededJSON, doc)
		if !same || err != nil {
			return datasources.SubmitResult{}, false, err
		}
		return datasources.SubmitResult{RevisionID: c.signedID(t.superseded), Status: status, Replayed: true}, true,
			nil
	}
	key, _ := positiveID(t.currentID)
	current, err := c.q.GetRevision(ctx, key)
	if err != nil {
		return datasources.SubmitResult{}, false, fmt.Errorf("fetching current revision: %w", err)
	}
	if !current.AuthorUserID.Valid || current.AuthorUserID.String != req.Actor.UserID {
		return datasources.SubmitResult{}, false, nil
	}
	same, err := sameDocument(t.kind, current.SnapshotJson, doc)
	if !same || err != nil {
		return datasources.SubmitResult{}, false, err
	}

	return datasources.SubmitResult{RevisionID: t.currentID, Status: status, Replayed: true}, true, nil
}

// sameDocument is whether the stored document data, of kind, is doc. MySQL's JSON columns do not keep the stored
// text, so the documents are compared as this code encodes them.
func sameDocument(kind documentKind, data []byte, doc domain.AnyDocument) (bool, error) {
	stored, err := kind.decode(data)
	if err != nil {
		return false, fmt.Errorf("revision snapshot: %w", err)
	}
	a, err := encodeJSON(stored)
	if err != nil {
		return false, err
	}
	b, err := encodeJSON(doc)
	if err != nil {
		return false, err
	}

	return bytes.Equal(a, b), nil
}

// revertSummary is the summary of the revision a revert creates: the request's, or by default
// "Revert to revision <id>.".
func revertSummary(req datasources.RevertRequest, t revertTarget) string {
	if req.Summary != "" {
		return req.Summary
	}

	return fmt.Sprintf("Revert to revision %d.", t.row.ID)
}

// insertRevert inserts the revision of a planned revert, pending, on the entity's current revision: the full
// document, its author's projection of it and that projection's keys.
func (c *catalogueReader) insertRevert(
	ctx context.Context, req datasources.RevertRequest, t revertTarget, plan revertPlan,
) (uint64, error) {
	snapshot, err := encodeJSON(plan.document)
	if err != nil {
		return 0, err
	}
	submitted, err := encodeJSON(plan.submitted)
	if err != nil {
		return 0, err
	}
	flagsJSON, err := encodeFlags(plan.flags)
	if err != nil {
		return 0, err
	}
	keys, err := encodeJSON(plan.received)
	if err != nil {
		return 0, err
	}
	id, err := c.q.InsertRevertRevision(ctx, queries.InsertRevertRevisionParams{
		EntityType: t.row.EntityType, EntityID: t.row.EntityID, BaseRevisionID: nullableID(&t.currentID),
		AuthorUserID: sql.NullString{String: req.Actor.UserID, Valid: true}, SnapshotJson: snapshot,
		SubmittedSnapshotJson: submitted, FlagsJson: flagsJSON, ReceivedKeysJson: keys,
		Summary: revertSummary(req, t), CreatedAt: req.Now, SubmittedAt: nullTime(req.Now),
	})
	if err != nil {
		return 0, fmt.Errorf("storing revert: %w", err)
	}
	key, ok := positiveID(id)
	if !ok {
		return 0, fmt.Errorf("revert revision [%d]: %w", id, errOutOfRange)
	}

	return key, nil
}
