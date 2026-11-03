package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql/queries"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ datasources.RevisionApprover = (*Repository)(nil)

// ApproveRevision approves a pending revision (D15) in one transaction, in the global lock order: the revision row,
// the revisions creating the pending new terms and persons it references, the entity rows (its own and those
// placeholders FOR UPDATE, every referenced entity FOR SHARE), the terms of a facet whose closure it rebuilds, and
// the links rows it writes. Nothing is searched or served from inside the transaction: callers read the result
// after it commits.
func (r *Repository) ApproveRevision(ctx context.Context, req datasources.ApproveRequest) error {
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		c := r.txReader(tx)
		row, err := c.lockModeratedRevision(ctx, req.Actor, req.RevisionID, domain.RevisionActionApprove)
		if err != nil {
			return err
		}
		in, err := c.pendingApproval(ctx, row.ID)
		if err != nil {
			return err
		}
		in.reviewer, in.options, in.duplicates, in.now = req.Actor.UserID, req.Options, req.Duplicates, req.Now

		return c.approve(ctx, in)
	})
	if err != nil {
		return withoutServerMessage(fmt.Sprintf("approving revision [%d]", req.RevisionID), err)
	}

	return nil
}

// lockModeratedRevision locks the revision id FOR UPDATE and checks the actor may take the moderation action on it
// (D2), returning datasources.ErrNotFound if there is no such revision (or it is a draft, which moderators never
// see).
func (c *catalogueReader) lockModeratedRevision(
	ctx context.Context, actor domain.Actor, id int64, action domain.RevisionAction,
) (queries.LockRevisionsRow, error) {
	key, ok := positiveID(id)
	if !ok {
		return queries.LockRevisionsRow{}, datasources.ErrNotFound
	}
	rows, err := c.q.LockRevisions(ctx, []uint64{key})
	if err != nil {
		return queries.LockRevisionsRow{}, fmt.Errorf("locking revision: %w", err)
	}
	if len(rows) == 0 || rows[0].Status == queries.RevisionsStatusDraft {
		return queries.LockRevisionsRow{}, datasources.ErrNotFound
	}
	if _, err := checkTransition(actor, rows[0], action); err != nil {
		return queries.LockRevisionsRow{}, err
	}

	return rows[0], nil
}

// approvalInput is a pending revision to approve, locked, and the moderator's approval of it.
type approvalInput struct {
	revisionID uint64
	authorID   string
	// author is the role the revision was submitted with: a contributor's, unless an interactive moderator's
	// submission or revert is approved at once.
	author         domain.Role
	entity         domain.EntityRef
	kind           documentKind
	doc            domain.AnyDocument
	baseRevisionID *int64
	summary        string
	// fallbackSummary is published if the approved summary is empty: a revert's default summary, which its
	// validation does not see (D10).
	fallbackSummary string
	storedFlags     []domain.Flag
	reviewer        string
	options         domain.ApprovalOptions
	duplicates      []domain.DuplicateMatch
	now             time.Time
}

// pendingApproval reads the locked pending revision id as an approvalInput, without the moderator's part.
func (c *catalogueReader) pendingApproval(ctx context.Context, id uint64) (approvalInput, error) {
	row, err := c.q.GetRevision(ctx, id)
	if err != nil {
		return approvalInput{}, fmt.Errorf("fetching revision: %w", err)
	}
	entityType := enumOf[domain.EntityType](c, row.EntityType)
	kind, err := kindFor(entityType)
	if err != nil {
		return approvalInput{}, err
	}
	doc, err := kind.decode(row.SnapshotJson)
	if err != nil {
		return approvalInput{}, fmt.Errorf("revision snapshot: %w", err)
	}
	flags, err := decodeFlags(row.FlagsJson)
	if err != nil {
		return approvalInput{}, err
	}

	return approvalInput{
		revisionID: row.ID, authorID: row.AuthorUserID.String, author: domain.RoleContributor,
		entity: domain.EntityRef{Type: entityType, ID: row.EntityID.Int64}, kind: kind, doc: doc,
		baseRevisionID: int64Ptr(row.BaseRevisionID), summary: row.Summary, storedFlags: flags,
	}, c.err
}

// decodeFlags decodes a revision's stored flags; a revision with none stored has none.
func decodeFlags(data []byte) ([]domain.Flag, error) {
	var flags []domain.Flag
	if len(data) == 0 {
		return flags, nil
	}
	if err := json.Unmarshal(data, &flags); err != nil {
		return nil, fmt.Errorf("decoding flags: %w", err)
	}

	return flags, nil
}

// approve approves the locked pending revision in (D15 steps 2 to 8): it finds and locks the pending new terms and
// persons the revision references and their creating revisions, locks the entity rows and the facet terms, plans
// the approval of each (domain.PlanApproval), and writes them, the pending references first.
func (c *catalogueReader) approve(ctx context.Context, in approvalInput) error {
	pending, err := c.lockPendingReferences(ctx, in)
	if err != nil {
		return err
	}
	written := []domain.EntityRef{in.entity}
	refs := referencesOf(in.kind, in.entity.ID, in.doc)
	for _, p := range pending {
		written = append(written, p.entity)
		p.kind.addReferences(p.entity.ID, p.doc, &refs)
	}
	rows, err := c.lockEntities(ctx, written, refs)
	if err != nil {
		return err
	}
	own, ok := rows[in.entity]
	if !ok {
		return fmt.Errorf("%s [%d]: %w", in.entity.Type, in.entity.ID, datasources.ErrEntityGone)
	}
	pending = slices.DeleteFunc(pending, func(p approvalInput) bool {
		row, ok := rows[p.entity]
		return !ok || row.currentRevision != nil
	})
	facets, err := c.lockFacets(ctx, in, pending)
	if err != nil {
		return err
	}

	docs, err := c.approvalDocuments(ctx, in, own)
	if err != nil {
		return err
	}
	plans, err := c.planApprovals(ctx, in, docs, pending)
	if err != nil {
		return err
	}

	return c.writeApprovals(ctx, docs, plans, facets)
}

// referencesOf returns what doc, a document of the entity id of kind, references.
func referencesOf(kind documentKind, id int64, doc domain.AnyDocument) domain.SnapshotReferences {
	var refs domain.SnapshotReferences
	kind.addReferences(id, doc, &refs)

	return refs
}

// lockPendingReferences finds the pending new terms and persons of the revision's author that its document
// references, directly or through one another's documents (a pending term's pending parent), and locks their
// creating revisions FOR UPDATE by ascending ID. It returns those still pending, each one after the pending
// entities it references (parents first). A reference to anything else that is not public is the document's own
// validation's to refuse (unknown_reference).
func (c *catalogueReader) lockPendingReferences(ctx context.Context, in approvalInput) ([]approvalInput, error) {
	order := pendingReferenceOrder(in.entity, referencesOf(in.kind, in.entity.ID, in.doc),
		c.pendingCandidates(ctx, in.authorID))
	if len(order) == 0 || c.err != nil {
		return nil, c.err
	}
	ids := make([]uint64, len(order))
	for i, e := range order {
		ids[i] = e.revisionID
	}
	rows, err := c.q.LockRevisions(ctx, slices.Sorted(slices.Values(ids)))
	if err != nil {
		return nil, fmt.Errorf("locking pending references: %w", err)
	}
	locked := map[uint64]bool{}
	for _, row := range rows {
		locked[row.ID] = row.Status == queries.RevisionsStatusPending
	}
	var out []approvalInput
	for _, e := range order {
		if !locked[e.revisionID] {
			continue
		}
		p, err := c.pendingApproval(ctx, e.revisionID)
		if err != nil {
			return nil, err
		}
		if p.entity != e.entity || p.authorID != in.authorID {
			continue
		}
		p.reviewer, p.now = in.reviewer, in.now
		out = append(out, p)
	}

	return out, nil
}

// pendingCandidates returns the pending new terms and persons of the user authorID, which their revisions may
// reference, by entity.
func (c *catalogueReader) pendingCandidates(
	ctx context.Context, authorID string,
) map[domain.EntityRef]pendingNewEntity {
	candidates := map[domain.EntityRef]pendingNewEntity{}
	for _, e := range c.pendingNewEntities(ctx, authorID, false) {
		if e.entity.Type == domain.EntityTypeTerm || e.entity.Type == domain.EntityTypePerson {
			candidates[e.entity] = e
		}
	}

	return candidates
}

// pendingReferenceOrder returns the candidates (the author's pending new terms and persons) that refs, the
// references of the entity own's document, reach, each after those its own document references.
func pendingReferenceOrder(
	own domain.EntityRef, refs domain.SnapshotReferences, candidates map[domain.EntityRef]pendingNewEntity,
) []pendingNewEntity {
	visited := map[domain.EntityRef]bool{own: true}
	var order []pendingNewEntity
	var visit func(refs domain.SnapshotReferences)
	visit = func(refs domain.SnapshotReferences) {
		var reached []domain.EntityRef
		for _, id := range refs.TermIDs {
			reached = append(reached, domain.EntityRef{Type: domain.EntityTypeTerm, ID: id})
		}
		for _, id := range refs.PersonIDs {
			reached = append(reached, domain.EntityRef{Type: domain.EntityTypePerson, ID: id})
		}
		for _, e := range reached {
			p, ok := candidates[e]
			if visited[e] || !ok {
				continue
			}
			visited[e] = true
			kind := documentKinds[e.Type]
			visit(referencesOf(kind, e.ID, p.doc))
			order = append(order, p)
		}
	}
	visit(refs)

	return order
}

// lockFacets locks every term of each facet a term document being approved belongs to (D15 step 5): the facets
// whose closure an approval may rebuild. It returns each locked facet's terms' parents, by ID.
func (c *catalogueReader) lockFacets(
	ctx context.Context, in approvalInput, pending []approvalInput,
) (map[domain.Facet]map[int64]*int64, error) {
	var facets []domain.Facet
	for _, doc := range append([]domain.AnyDocument{in.doc}, pendingDocuments(pending)...) {
		if d, ok := doc.(domain.TermDocument); ok {
			facets = append(facets, d.Facet)
		}
	}
	out := map[domain.Facet]map[int64]*int64{}
	for _, facet := range slices.Compact(slices.Sorted(slices.Values(facets))) {
		rows, err := c.q.LockTermsOfFacet(ctx, queries.TermsFacet(facet))
		if err != nil {
			return nil, fmt.Errorf("locking the terms of facet %s: %w", facet, err)
		}
		parents := map[int64]*int64{}
		for _, row := range rows {
			parents[c.signedID(row.ID)] = int64Ptr(row.ParentID)
		}
		out[facet] = parents
	}

	return out, c.err
}

// pendingDocuments returns the documents of the pending references.
func pendingDocuments(pending []approvalInput) []domain.AnyDocument {
	docs := make([]domain.AnyDocument, len(pending))
	for i, p := range pending {
		docs[i] = p.doc
	}

	return docs
}

// approvalDocuments are the documents an approval compares the revision's with: those of its base and of the
// entity's current revision, nil for none.
type approvalDocuments struct {
	base, current     domain.AnyDocument
	currentRevisionID *int64
}

// approvalDocuments loads the base and current documents of the revision in, whose entity's locked row is own,
// and locks the rows of what the current document references FOR SHARE: a merge may keep them.
func (c *catalogueReader) approvalDocuments(
	ctx context.Context, in approvalInput, own entityRow,
) (approvalDocuments, error) {
	entity := domain.RevisionEntity{Type: in.entity.Type, ID: in.entity.ID}
	docs := approvalDocuments{currentRevisionID: own.currentRevision}
	var err error
	if own.currentRevision != nil {
		if docs.current, err = c.approvedDocument(ctx, in.kind, entity, *own.currentRevision); err != nil {
			return approvalDocuments{}, err
		}
		if _, err := c.lockEntities(ctx, nil, referencesOf(in.kind, in.entity.ID, docs.current)); err != nil {
			return approvalDocuments{}, err
		}
	}
	if in.baseRevisionID != nil {
		if docs.base, err = c.approvedDocument(ctx, in.kind, entity, *in.baseRevisionID); err != nil {
			return approvalDocuments{}, err
		}
	}

	return docs, nil
}

// plannedApproval is an approval planned: of the revision approved, its plan and the current document it replaces
// (nil for a new entity).
type plannedApproval struct {
	in      approvalInput
	plan    approvalPlan
	current domain.AnyDocument
}

// planApprovals plans the approval of the revision in and of each pending reference approved alongside it. A
// pending reference whose own approval would be refused refuses the approval with
// *domain.PendingReferenceFlaggedError: the moderator approves or rejects it on its own first (D15 step 4).
func (c *catalogueReader) planApprovals(
	ctx context.Context, in approvalInput, docs approvalDocuments, pending []approvalInput,
) ([]plannedApproval, error) {
	own := make([]pendingNewEntity, len(pending))
	refs := make([]domain.PendingReference, len(pending))
	for i, p := range pending {
		own[i] = pendingNewEntity{entity: p.entity, doc: p.doc}
		refs[i] = domain.PendingReference{Entity: p.entity, RevisionID: c.signedID(p.revisionID), Flags: p.storedFlags}
	}
	ownRefs := ownPendingRefs(own)
	hidden := fetchAll(ctx, c, "hidden aliases", c.q.ListHiddenAliasNames)

	state := c.approvalState(ctx, in, ownRefs, hidden, in.doc, docs.base, docs.current)
	state.currentRevisionID = docs.currentRevisionID
	state.pendingReferences = refs
	if docs.current == nil {
		state.possibleDuplicate = len(in.duplicates) > 0
		state.duplicateBlocked = slices.ContainsFunc(in.duplicates, func(m domain.DuplicateMatch) bool {
			return m.Blocked
		})
	}
	if c.err != nil {
		return nil, c.err
	}
	plan, err := in.kind.planApproval(state, in.options)
	if err != nil {
		return nil, err
	}

	var reviewNoteAcknowledged []domain.FlagCode
	if slices.Contains(in.options.AcknowledgedFlags, domain.FlagHiddenAliasInReviewNote) {
		reviewNoteAcknowledged = []domain.FlagCode{domain.FlagHiddenAliasInReviewNote}
	}
	plans := make([]plannedApproval, 0, len(pending)+1)
	for i, p := range pending {
		s := c.approvalState(ctx, p, ownRefs, hidden, p.doc, nil, nil)
		if c.err != nil {
			return nil, c.err
		}
		refPlan, err := p.kind.planApproval(s, domain.ApprovalOptions{AcknowledgedFlags: reviewNoteAcknowledged,
			ReviewNote: in.options.ReviewNote})
		if err != nil {
			return nil, pendingReferenceRefused(refs[i], err)
		}
		p.options = in.options
		plans = append(plans, plannedApproval{in: p, plan: refPlan})
	}

	return append(plans, plannedApproval{in: in, plan: plan, current: docs.current}), nil
}

// pendingReferenceRefused is the error of an approval whose pending reference ref would be refused with err on its
// own: a *domain.PendingReferenceFlaggedError naming it, with the flags its approval would need acknowledged, if
// any, else its stored flags. Other errors pass through.
func pendingReferenceRefused(ref domain.PendingReference, err error) error {
	var unacknowledged *domain.UnacknowledgedFlagsError
	var problems *domain.DocumentError
	switch {
	case errors.As(err, &unacknowledged):
		return &domain.PendingReferenceFlaggedError{RevisionID: ref.RevisionID, Flags: unacknowledged.Flags}
	case errors.As(err, &problems), errors.Is(err, domain.ErrHiddenDataConflict),
		errors.Is(err, domain.ErrDuplicateBlocked):
		return &domain.PendingReferenceFlaggedError{RevisionID: ref.RevisionID, Flags: ref.Flags}
	}

	return err
}

// approvalState loads what planning the approval of in's document doc needs, its author's pending references
// being own: what validation knows of what doc (and base and current, if not nil) references, and the hidden
// links among doc's links.
func (c *catalogueReader) approvalState(
	ctx context.Context, in approvalInput, own domain.OwnPendingRefs, hidden []string,
	doc, base, current domain.AnyDocument,
) approvalState {
	refs := referencesOf(in.kind, in.entity.ID, doc)
	for _, d := range []domain.AnyDocument{base, current} {
		if d != nil {
			in.kind.addReferences(in.entity.ID, d, &refs)
		}
	}
	hiddenLinks := map[string]bool{}
	for _, state := range c.linkStates(ctx, refs.LinkURLs) {
		if state.isHidden {
			hiddenLinks[state.normalizedURL] = true
		}
	}

	return approvalState{
		id: in.entity.ID, revision: doc, base: base, current: current, baseRevisionID: in.baseRevisionID,
		author: in.author, refs: c.referenceIndex(ctx, refs, own, doc), hiddenAliases: hidden,
		hiddenLinkURLs: hiddenLinks, summary: in.summary, storedFlags: in.storedFlags, today: dateOf(in.now),
	}
}

// writeApprovals writes the planned approvals, in order (D15 steps 5 to 8): each entity's rows, its conservation
// history, the closure of each facet given new parents, then each entity's current revision and its revision
// approved. A missing foreign-key parent (MySQL error 1452: a referenced placeholder was deleted) is
// datasources.ErrEntityGone.
func (c *catalogueReader) writeApprovals(
	ctx context.Context, docs approvalDocuments, plans []plannedApproval,
	facets map[domain.Facet]map[int64]*int64,
) error {
	w := &entityWriter{ctx: ctx, q: c.q}
	rebuilt := map[domain.Facet]bool{}
	for _, p := range plans {
		w.write(approvedEntity{entity: p.in.entity, doc: p.plan.document, current: p.current})
		if change := p.plan.conservationChange; change != nil {
			w.conservationChange(p.in.entity.ID, *change, c.signedID(p.in.revisionID), p.in.now)
		}
		if term, ok := p.plan.document.(domain.TermDocument); ok {
			facets[term.Facet][p.in.entity.ID] = term.ParentID
			if p.plan.termFacetRebuild != nil {
				rebuilt[*p.plan.termFacetRebuild] = true
			}
		}
	}
	for _, facet := range slices.Sorted(maps.Keys(rebuilt)) {
		w.rebuildClosure(facets[facet])
	}
	for _, p := range plans {
		base := p.in.baseRevisionID
		if p.plan.rebased {
			base = docs.currentRevisionID
		}
		c.writeApprovedRevision(w, p, base)
	}
	if w.err != nil {
		return approvalWriteError(w.err)
	}

	return c.err
}

// approvalWriteError is the error of an approval's writes: a missing foreign-key parent (MySQL error 1452, a
// referenced entity deleted) is datasources.ErrEntityGone, never an internal error.
func approvalWriteError(err error) error {
	var writeErr *writeError
	if errors.As(err, &writeErr) && mysqlErrorNumber(writeErr.err) == erNoReferencedRow {
		return fmt.Errorf("%w: writing %s: MySQL error %d", datasources.ErrEntityGone, writeErr.table,
			erNoReferencedRow)
	}

	return err
}

// writeApprovedRevision makes the approved revision of p its entity's current revision, and stores it approved
// with its plan's document and summary, on base, dated by its approval (D1).
func (c *catalogueReader) writeApprovedRevision(w *entityWriter, p plannedApproval, base *int64) {
	snapshot, err := encodeJSON(p.plan.document)
	if err != nil {
		w.fail(err)
		return
	}
	summary := p.plan.summary
	if summary == "" {
		summary = p.in.fallbackSummary
	}
	current := sql.NullInt64{Int64: c.signedID(p.in.revisionID), Valid: true}
	key := w.id(p.in.entity.ID)
	setCurrent := map[domain.EntityType]func() (int64, error){
		domain.EntityTypePiece: func() (int64, error) {
			return c.q.SetCurrentRevisionPiece(w.ctx, queries.SetCurrentRevisionPieceParams{CurrentRevisionID: current,
				ID: key})
		},
		domain.EntityTypePerson: func() (int64, error) {
			return c.q.SetCurrentRevisionPerson(w.ctx, queries.SetCurrentRevisionPersonParams{
				CurrentRevisionID: current, ID: key})
		},
		domain.EntityTypeTerm: func() (int64, error) {
			return c.q.SetCurrentRevisionTerm(w.ctx, queries.SetCurrentRevisionTermParams{CurrentRevisionID: current,
				ID: key})
		},
		domain.EntityTypeCollection: func() (int64, error) {
			return c.q.SetCurrentRevisionCollection(w.ctx, queries.SetCurrentRevisionCollectionParams{
				CurrentRevisionID: current, ID: key})
		},
		domain.EntityTypeEvent: func() (int64, error) {
			return c.q.SetCurrentRevisionEvent(w.ctx, queries.SetCurrentRevisionEventParams{CurrentRevisionID: current,
				ID: key})
		},
	}
	w.exec(string(entityTables[p.in.entity.Type]), func() error {
		return requireMatched(setCurrent[p.in.entity.Type]())
	})
	w.exec("revisions", func() error {
		return requireMatched(c.q.ApproveRevisionRow(w.ctx, queries.ApproveRevisionRowParams{
			SnapshotJson: snapshot, BaseRevisionID: nullableID(base), Summary: summary,
			ReviewerUserID: sql.NullString{String: p.in.reviewer, Valid: true}, ApprovedAt: p.in.now,
			ReviewNote: optionalText(p.in.options.ReviewNote), ID: p.in.revisionID,
		}))
	})
}

// optionalText is a text column's value, NULL for an empty string.
func optionalText(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// rebuildClosure replaces the closure rows of a facet's terms, given each term's parent (D15 step 5). A cycle or a
// term too deep, which validation refuses unless the facet changed under it, is a *domain.DocumentError.
func (w *entityWriter) rebuildClosure(parents map[int64]*int64) {
	if w.err != nil {
		return
	}
	ancestors, err := domain.TermClosure(parents)
	switch {
	case errors.Is(err, domain.ErrTermCycle):
		w.fail(&domain.DocumentError{Problems: []domain.DocumentProblem{{Path: "/parent", Code: domain.ProblemCycle}}})
		return
	case errors.Is(err, domain.ErrTermTooDeep):
		w.fail(&domain.DocumentError{Problems: []domain.DocumentProblem{{Path: "/parent",
			Code: domain.ProblemTooDeep}}})
		return
	case err != nil:
		w.fail(fmt.Errorf("rebuilding a facet's closure: %w", err))
		return
	}
	ids := unsignedIDs(slices.Sorted(maps.Keys(parents)))
	w.exec("term_ancestors", func() error { return w.q.DeleteFacetClosure(w.ctx, ids) })
	w.termAncestors(ancestors)
}
