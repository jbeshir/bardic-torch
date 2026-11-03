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

// RejectRevision rejects a pending revision (D2): it locks the revision, checks the review note (D19), then locks
// its entity's row, rejects it and deletes a pending new entity's placeholder if no other pending revision has it
// (D14).
func (r *Repository) RejectRevision(ctx context.Context, req datasources.RejectRequest) error {
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
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
	})
	if err != nil {
		return withoutServerMessage(fmt.Sprintf("rejecting revision [%d]", req.RevisionID), err)
	}

	return nil
}

// RebaseRevision rebases a pending revision onto its entity's current revision (D9): under the revision's lock and
// its entity row's, the moderator's resolved document must be against the current revision, is validated against
// the current document with the revision author's role (domain.PlanRebase), and replaces the revision's document,
// which stays pending; what its author submitted, and the base they edited from, are kept.
func (r *Repository) RebaseRevision(ctx context.Context, req datasources.RebaseRequest) error {
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
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
	})
	if err != nil {
		return withoutServerMessage(fmt.Sprintf("rebasing revision [%d]", req.RevisionID), err)
	}

	return nil
}

func (c *catalogueReader) rebase(ctx context.Context, req datasources.RebaseRequest, in approvalInput) error {
	own, err := c.lockOwnEntity(ctx, in.entity, referencesOf(in.kind, in.entity.ID, req.Document))
	if err != nil {
		return err
	}
	if !equalIDs(own.currentRevision, req.CurrentRevisionID) {
		return datasources.ErrStale
	}
	docs, err := c.approvalDocuments(ctx, approvalInput{entity: in.entity, kind: in.kind}, own)
	if err != nil {
		return err
	}
	current := docs.current
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
	doc, err := in.kind.planRebase(rebaseState{id: in.entity.ID, resolved: req.Document, revision: in.doc,
		current: current,
		author:  in.author, vis: vis, refs: index, summary: summary, today: dateOf(req.Now)})
	if err != nil {
		return err
	}
	snapshot, err := encodeJSON(doc)
	if err != nil {
		return err
	}
	err = requireMatched(c.q.RebaseRevisionRow(ctx, queries.RebaseRevisionRowParams{SnapshotJson: snapshot,
		BaseRevisionID: nullableID(own.currentRevision), Summary: summary,
		RebasedByUserID: sql.NullString{String: req.Actor.UserID, Valid: true}, RebasedAt: nullTime(req.Now),
		ID: in.revisionID}))
	if err != nil {
		return fmt.Errorf("storing rebase: %w", err)
	}

	return nil
}

// RevertTo reverts an entity to one of its approved revisions (D10), in the global lock order: the actor's users
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
		return datasources.SubmitResult{}, withoutServerMessage(fmt.Sprintf("reverting to revision [%d]",
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
	count, err := c.q.CountPendingRevisions(ctx, queries.CountPendingRevisionsParams{
		AuthorUserID: sql.NullString{String: actor.UserID, Valid: true}, ExcludedID: t.superseded,
	})
	if err != nil {
		return fmt.Errorf("counting pending revisions: %w", err)
	}
	if count >= domain.MaxOpenPendingRevisions {
		return datasources.ErrPendingLimitReached
	}
	if t.superseded == 0 {
		return nil
	}
	if err := requireMatched(c.q.WithdrawRevision(ctx, t.superseded)); err != nil {
		return fmt.Errorf("superseding revision [%d]: %w", t.superseded, err)
	}

	return nil
}

// revertStatus is the status of the revision a revert by the request's actor creates: pending, or approved for an
// interactive moderator (domain.CheckTransition on the approved target).
func (c *catalogueReader) revertStatus(req datasources.RevertRequest) (domain.RevisionStatus, error) {
	approved := domain.RevisionStatusApproved

	return domain.CheckTransition(domain.TransitionInput{Role: req.Actor.Role, From: &approved,
		Action: domain.RevisionActionRevert})
}

// lockRevert reads the revert's target, which must be an approved revision of a public entity, and takes the
// revert's locks (D10): for a pending revert the actor's users row and their pending revision of the entity, then
// the entity row and what the documents reference. The target being the current revision is
// datasources.ErrAlreadyCurrent.
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
	own, err := c.lockOwnEntity(ctx, t.entity, referencesOf(t.kind, t.entity.ID, t.doc))
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
	if t.current, err = c.approvedDocument(ctx, t.kind, domain.RevisionEntity{Type: t.entity.Type, ID: t.entity.ID},
		t.currentID); err != nil {
		return revertTarget{}, err
	}
	if _, err := c.lockEntities(ctx, nil, referencesOf(t.kind, t.entity.ID, t.current)); err != nil {
		return revertTarget{}, err
	}

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
	if t.kind, err = kindFor(t.entity.Type); err != nil {
		return revertTarget{}, err
	}
	if t.doc, err = t.kind.decode(row.SnapshotJson); err != nil {
		return revertTarget{}, fmt.Errorf("revision snapshot: %w", err)
	}

	return t, c.err
}

// lockSuperseded locks the actor's users row, then their pending revision of the target's entity, if any, which a
// pending revert supersedes: found by an unlocked read and checked again once locked.
func (c *catalogueReader) lockSuperseded(ctx context.Context, actor domain.Actor, t *revertTarget) error {
	if _, err := c.q.LockUser(ctx, actor.UserID); err != nil {
		return fmt.Errorf("locking user: %w", lookupError("user", err))
	}
	author := sql.NullString{String: actor.UserID, Valid: true}
	pending, err := c.q.GetUserPendingForEntity(ctx, queries.GetUserPendingForEntityParams{
		AuthorUserID: author, EntityType: t.row.EntityType, EntityID: t.row.EntityID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("fetching superseded revision: %w", err)
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

// revertReplay returns the revision a replayed revert already created (D10), and whether the revert is one: for a
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
// document, its author's projection of it and that projection's keys (D10).
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
	flags := plan.flags
	if flags == nil {
		flags = []domain.Flag{}
	}
	flagsJSON, err := encodeJSON(flags)
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

// equalIDs is whether two optional IDs are both nil or equal.
func equalIDs(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
