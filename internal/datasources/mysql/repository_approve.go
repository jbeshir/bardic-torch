package mysql

import (
	"context"
	"database/sql"
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

// ApproveRevision approves a pending revision (docs/PLAN.md §5.1 "Approval") in one transaction, in the global lock
// order: the moderator's users row (lockModerator: the block and the role checked again), the revision row, the
// revisions creating the pending new terms and persons it references, the entity rows (its own and those
// placeholders FOR UPDATE, what the revision and the current document reference FOR SHARE, and the terms of a facet
// whose closure it may rebuild, first among the terms), and the links rows it writes. Nothing is searched or served
// from inside the transaction: callers read the result after it commits.
func (r *Repository) ApproveRevision(ctx context.Context, req datasources.ApproveRequest) error {
	err := r.withWriteTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		return r.txReader(tx).approveRevision(ctx, req)
	})
	if err != nil {
		return databaseError(fmt.Sprintf("approving revision [%d]", req.RevisionID), err)
	}

	return nil
}

// approveRevision is ApproveRevision's transaction.
func (c *catalogueReader) approveRevision(ctx context.Context, req datasources.ApproveRequest) error {
	if err := c.lockModerator(ctx, req.Actor.UserID); err != nil {
		return err
	}
	row, err := c.lockModeratedRevision(ctx, req.Actor, req.RevisionID, domain.RevisionActionApprove)
	if err != nil {
		return err
	}
	in, err := c.pendingApproval(ctx, row.ID)
	if err != nil {
		return err
	}
	in.reviewer, in.options, in.now = req.Actor.UserID, req.Options, req.Now
	in.duplicates = map[uint64]datasources.DuplicateCheck{}
	for id, check := range req.Duplicates {
		if key, ok := positiveID(id); ok {
			in.duplicates[key] = check
		}
	}

	pending, err := c.lockPendingReferences(ctx, in)
	if err != nil {
		return err
	}

	return c.approve(ctx, in, pending)
}

// lockModeratedRevision locks the revision id FOR UPDATE and checks the actor may take the moderation action on it
// , returning datasources.ErrNotFound if there is no such revision (or it is a draft, which moderators never
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
	author domain.Role
	entity domain.EntityRef
	// slug is a pending reference's placeholder slug, empty for the revision approved.
	slug           string
	kind           documentKind
	doc            domain.AnyDocument
	baseRevisionID *int64
	summary        string
	// fallbackSummary is published if the approved summary is empty: a revert's default summary, which its
	// validation does not see.
	fallbackSummary string
	storedFlags     []domain.Flag
	reviewer        string
	options         domain.ApprovalOptions
	// duplicates are the duplicate checks of the new entities the approval may approve, by revision ID
	// (datasources.ApproveRequest): its own entity's and those of the pending references approved alongside, which an
	// inline approval, of an interactive moderator's own submission, takes from datasources.SubmitRequest.
	duplicates map[uint64]datasources.DuplicateCheck
	now        time.Time
}

// pendingApproval reads the locked pending revision id as an approvalInput, without the moderator's part.
func (c *catalogueReader) pendingApproval(ctx context.Context, id uint64) (approvalInput, error) {
	row, err := c.q.GetRevision(ctx, id)
	if err != nil {
		return approvalInput{}, fmt.Errorf("fetching revision: %w", err)
	}
	entityType := enumOf[domain.EntityType](c, row.EntityType)
	kind, doc, err := decodeRevision(entityType, row.SnapshotJson)
	if err != nil {
		return approvalInput{}, fmt.Errorf("revision [%d]: %w", id, err)
	}
	flags, err := decodeJSONColumn[[]domain.Flag]("flags", row.FlagsJson)
	if err != nil {
		return approvalInput{}, err
	}

	return approvalInput{
		revisionID: row.ID, authorID: row.AuthorUserID.String, author: domain.RoleContributor,
		entity: domain.EntityRef{Type: entityType, ID: row.EntityID.Int64}, kind: kind, doc: doc,
		baseRevisionID: int64Ptr(row.BaseRevisionID), summary: row.Summary, storedFlags: flags,
	}, c.err
}

// approve approves the locked pending revision in (docs/PLAN.md §5.1 "Approval"), with pending, the pending new terms
// and persons it references whose creating revisions the caller locked (lockPendingReferences). It locks the entity
// rows: its own, those placeholders and the terms of the facets a term document approved belongs to FOR UPDATE, and
// what the revision, the pending references and the entity's current document reference FOR SHARE. It plans the
// approval of each (domain.PlanApproval), and writes them, the pending references first.
func (c *catalogueReader) approve(ctx context.Context, in approvalInput, pending []approvalInput) error {
	written := []domain.EntityRef{in.entity}
	refs := referencesOf(in.kind, in.entity.ID, in.doc)
	for _, p := range pending {
		written = append(written, p.entity)
		p.kind.addReferences(p.entity.ID, p.doc, &refs)
	}
	peeked, err := c.peekCurrent(ctx, in.kind, in.entity)
	if err != nil {
		return err
	}
	peeked.references(in.kind, in.entity.ID, &refs)
	locked, err := c.lockEntities(ctx, entityLocks{written: written, refs: refs,
		facets: rebuiltFacets(append([]domain.AnyDocument{in.doc}, pendingDocuments(pending)...)...)})
	if err != nil {
		return err
	}
	own, ok := locked.rows[in.entity]
	if !ok {
		return fmt.Errorf("%s [%d]: %w", in.entity.Type, in.entity.ID, datasources.ErrEntityGone)
	}
	pending = slices.DeleteFunc(pending, func(p approvalInput) bool {
		row, ok := locked.rows[p.entity]
		return !ok || row.currentRevision != nil
	})

	docs, err := c.approvalDocuments(ctx, in, own, peeked)
	if err != nil {
		return err
	}
	plans, err := c.planApprovals(ctx, in, docs, pending)
	if err != nil {
		return err
	}

	return c.writeApprovals(ctx, docs, plans, locked.facets)
}

// referencesOf returns what doc, a document of the entity id of kind, references.
func referencesOf(kind documentKind, id int64, doc domain.AnyDocument) domain.SnapshotReferences {
	var refs domain.SnapshotReferences
	kind.addReferences(id, doc, &refs)

	return refs
}

// maxPendingReferenceRounds is how many times lockPendingReferences locks newly found pending references before it
// gives up with datasources.ErrDocumentChanged.
const maxPendingReferenceRounds = 3

// lockPendingReferences finds the pending new terms and persons of the revision's author that its document
// references, directly or through one another's documents (a pending term's pending parent), and locks their
// creating revisions FOR UPDATE by ascending ID. The candidates come from an unlocked read, so once they are locked
// it reads them again, and locks any it finds anew (a revision superseding one it locked), until a read finds no
// new one; after maxPendingReferenceRounds it gives up with datasources.ErrDocumentChanged. It returns those still
// pending, each one after the pending entities it references (parents first). A reference to anything else that is
// not public is the document's own validation's to refuse (unknown_reference).
func (c *catalogueReader) lockPendingReferences(ctx context.Context, in approvalInput) ([]approvalInput, error) {
	refs := referencesOf(in.kind, in.entity.ID, in.doc)
	pending := map[uint64]bool{}
	var order []pendingNewEntity
	for round := 0; ; round++ {
		order = pendingReferenceOrder(in.entity, refs, c.pendingCandidates(ctx, in.authorID))
		if c.err != nil {
			return nil, c.err
		}
		found := slices.DeleteFunc(revisionIDs(order), func(id uint64) bool {
			_, locked := pending[id]
			return locked
		})
		if len(found) == 0 {
			break
		}
		if round == maxPendingReferenceRounds {
			return nil, fmt.Errorf("%w: the pending references kept changing", datasources.ErrDocumentChanged)
		}
		if err := c.lockPendingRevisions(ctx, found, pending); err != nil {
			return nil, err
		}
	}

	var out []approvalInput
	for _, e := range order {
		if !pending[e.revisionID] {
			continue
		}
		p, err := c.pendingApproval(ctx, e.revisionID)
		if err != nil {
			return nil, err
		}
		if p.entity != e.entity || p.authorID != in.authorID {
			continue
		}
		p.slug, p.reviewer, p.now = e.slug, in.reviewer, in.now
		out = append(out, p)
	}

	return out, nil
}

// revisionIDs returns the IDs of the revisions creating the pending new entities.
func revisionIDs(entities []pendingNewEntity) []uint64 {
	ids := make([]uint64, len(entities))
	for i, e := range entities {
		ids[i] = e.revisionID
	}

	return ids
}

// lockPendingRevisions locks the revisions ids FOR UPDATE by ascending ID, recording in pending whether each is
// still pending.
func (c *catalogueReader) lockPendingRevisions(ctx context.Context, ids []uint64, pending map[uint64]bool) error {
	rows, err := c.q.LockRevisions(ctx, slices.Sorted(slices.Values(ids)))
	if err != nil {
		return fmt.Errorf("locking pending references: %w", err)
	}
	for _, id := range ids {
		pending[id] = false
	}
	for _, row := range rows {
		pending[row.ID] = row.Status == queries.RevisionsStatusPending
	}

	return nil
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

// approvalDocuments loads the base document of the revision in, and returns it with the current document of its
// entity, whose locked row is own, peeked being the current document read before the lock (lockedCurrent).
func (c *catalogueReader) approvalDocuments(
	ctx context.Context, in approvalInput, own entityRow, peeked currentDocument,
) (approvalDocuments, error) {
	current, err := c.lockedCurrent(ctx, in.kind, in.entity, own, peeked)
	if err != nil {
		return approvalDocuments{}, err
	}
	docs := approvalDocuments{current: current.doc, currentRevisionID: current.id}
	if in.baseRevisionID != nil {
		entity := domain.RevisionEntity{Type: in.entity.Type, ID: in.entity.ID}
		if docs.base, err = c.approvedDocument(ctx, in.kind, entity, *in.baseRevisionID); err != nil {
			return approvalDocuments{}, err
		}
	}

	return docs, nil
}

// plannedApproval is an approval planned: of the revision approved, the document to write, the rest of its plan
// and the current document it replaces (nil for a new entity).
type plannedApproval struct {
	in       approvalInput
	document domain.AnyDocument
	outcome  domain.ApprovalOutcome
	current  domain.AnyDocument
}

// planApprovals plans the approval of the revision in and of each pending reference approved alongside it. A
// pending reference whose own approval would be refused, or which may duplicate another entity, refuses the approval
// with *domain.PendingReferenceFlaggedError: the moderator approves or rejects it on its own first (docs/PLAN.md
// §5.1 "Approval"), and so does an inline approval's. A new entity approved whose duplicate check is missing or of
// other names refuses it with datasources.ErrDocumentChanged.
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

	inputs := c.approvalInputs(ctx, in, ownRefs, hidden, in.doc, docs.base, docs.current)
	inputs.CurrentRevisionID = docs.currentRevisionID
	inputs.PendingReferences = refs
	if docs.current == nil {
		if err := in.checkDuplicates(in.revisionID, in.doc, &inputs); err != nil {
			return nil, err
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	doc, outcome, err := in.kind.planApproval(inputs, in.doc, docs.base, docs.current, in.options)
	if err != nil {
		return nil, err
	}

	var reviewNoteAcknowledged []domain.FlagCode
	if slices.Contains(in.options.AcknowledgedFlags, domain.FlagHiddenAliasInReviewNote) {
		reviewNoteAcknowledged = []domain.FlagCode{domain.FlagHiddenAliasInReviewNote}
	}
	plans := make([]plannedApproval, 0, len(pending)+1)
	for i, p := range pending {
		refInputs := c.approvalInputs(ctx, p, ownRefs, hidden, p.doc, nil, nil)
		if c.err != nil {
			return nil, c.err
		}
		if err := in.checkDuplicates(p.revisionID, p.doc, &refInputs); err != nil {
			return nil, err
		}
		refDoc, refOutcome, err := p.kind.planApproval(refInputs, p.doc, nil, nil,
			domain.ApprovalOptions{AcknowledgedFlags: reviewNoteAcknowledged, ReviewNote: in.options.ReviewNote})
		if err != nil {
			return nil, pendingReferenceRefused(refs[i], err)
		}
		p.options = in.options
		plans = append(plans, plannedApproval{in: p, document: refDoc, outcome: refOutcome})
	}

	return append(plans, plannedApproval{in: in, document: doc, outcome: outcome, current: docs.current}), nil
}

// checkDuplicates sets the duplicate state of the approval of the new entity whose revision revisionID holds the
// locked document doc from in's duplicate check of it: possible_duplicate for any match, and blocked for a match on
// a person who prefers no page. A check that is missing, or of other names than doc's, is
// datasources.ErrDocumentChanged.
func (in approvalInput) checkDuplicates(
	revisionID uint64, doc domain.AnyDocument, inputs *domain.ApprovalInputs,
) error {
	check, ok := in.duplicates[revisionID]
	if !ok || !check.Candidate.Equal(domain.NewDuplicateCandidate(doc)) {
		return fmt.Errorf("duplicate check of revision [%d]: %w", revisionID, datasources.ErrDocumentChanged)
	}
	inputs.PossibleDuplicate = len(check.Matches) > 0
	inputs.DuplicateBlocked = slices.ContainsFunc(check.Matches, func(m domain.DuplicateMatch) bool { return m.Blocked })

	return nil
}

// uncheckedPendingReferences refuses the inline approval of in, approving the locked pending references pending
// alongside, if the duplicate check of any of them is missing or of other names than its locked document's, with a
// *datasources.UncheckedPendingReferencesError naming each such reference.
func (c *catalogueReader) uncheckedPendingReferences(in approvalInput, pending []approvalInput) error {
	unchecked := map[int64]datasources.PendingReferenceCandidate{}
	for _, p := range pending {
		candidate := domain.NewDuplicateCandidate(p.doc)
		if check, ok := in.duplicates[p.revisionID]; !ok || !check.Candidate.Equal(candidate) {
			unchecked[c.signedID(p.revisionID)] = datasources.PendingReferenceCandidate{
				Entity:    domain.RevisionEntity{Type: p.entity.Type, ID: p.entity.ID, Slug: p.slug, IsNew: true},
				Candidate: candidate,
			}
		}
	}
	if len(unchecked) > 0 {
		return &datasources.UncheckedPendingReferencesError{References: unchecked}
	}

	return nil
}

// pendingReferenceRefused is the error of an approval whose pending reference ref would be refused with err on its
// own: a *domain.PendingReferenceFlaggedError naming it, with the flags its approval would need acknowledged, if
// any, else its stored flags, and possible_duplicate if it duplicates a person who prefers no page. Other errors
// pass through.
func pendingReferenceRefused(ref domain.PendingReference, err error) error {
	var unacknowledged *domain.UnacknowledgedFlagsError
	var problems *domain.DocumentError
	switch {
	case errors.As(err, &unacknowledged):
		return &domain.PendingReferenceFlaggedError{RevisionID: ref.RevisionID, Flags: unacknowledged.Flags}
	case errors.Is(err, domain.ErrDuplicateBlocked):
		flags := ref.Flags
		if !slices.ContainsFunc(flags, func(f domain.Flag) bool { return f.Code == domain.FlagPossibleDuplicate }) {
			flags = append(slices.Clone(flags), domain.Flag{Code: domain.FlagPossibleDuplicate})
		}
		return &domain.PendingReferenceFlaggedError{RevisionID: ref.RevisionID, Flags: flags}
	case errors.As(err, &problems), errors.Is(err, domain.ErrHiddenDataConflict):
		return &domain.PendingReferenceFlaggedError{RevisionID: ref.RevisionID, Flags: ref.Flags}
	}

	return err
}

// approvalInputs loads what planning the approval of in's document doc needs besides the documents, its author's
// pending references being own: what validation knows of what doc (and base and current, if not nil) references,
// and the hidden links among doc's links.
func (c *catalogueReader) approvalInputs(
	ctx context.Context, in approvalInput, own domain.OwnPendingRefs, hidden []string,
	doc, base, current domain.AnyDocument,
) domain.ApprovalInputs {
	refs := referencesOf(in.kind, in.entity.ID, doc)
	for _, d := range []domain.AnyDocument{base, current} {
		if d != nil {
			in.kind.addReferences(in.entity.ID, d, &refs)
		}
	}

	return domain.ApprovalInputs{
		ID: in.entity.ID, BaseRevisionID: in.baseRevisionID, Author: in.author,
		Refs: c.referenceIndex(ctx, refs, own, doc), HiddenAliases: hidden, HiddenLinkURLs: c.hiddenLinkURLs(ctx, refs),
		Summary: in.summary, StoredFlags: in.storedFlags, Today: dateOf(in.now),
	}
}

// writeApprovals writes the planned approvals, in order (docs/PLAN.md §5.1 "Approval"): the links rows of every
// approved document in one ascending pass, each entity's rows, its conservation history, the closure of each facet
// given new parents, then each entity's current revision and its revision approved. A missing foreign-key parent
// (MySQL error 1452: a referenced placeholder was deleted) is datasources.ErrEntityGone.
func (c *catalogueReader) writeApprovals(
	ctx context.Context, docs approvalDocuments, plans []plannedApproval,
	facets map[domain.Facet]map[int64]*int64,
) error {
	w := &entityWriter{ctx: ctx, q: c.q}
	entities := make([]approvedEntity, len(plans))
	for i, p := range plans {
		entities[i] = approvedEntity{entity: p.in.entity, doc: p.document, current: p.current}
	}
	w.writeAll(entities)
	rebuilt := map[domain.Facet]bool{}
	for _, p := range plans {
		if change := p.outcome.ConservationChange; change != nil {
			w.conservationChange(p.in.entity.ID, *change, c.signedID(p.in.revisionID), p.in.now)
		}
		if term, ok := p.document.(domain.TermDocument); ok {
			facets[term.Facet][p.in.entity.ID] = term.ParentID
			if p.outcome.TermFacetRebuild != nil {
				rebuilt[*p.outcome.TermFacetRebuild] = true
			}
		}
	}
	for _, facet := range slices.Sorted(maps.Keys(rebuilt)) {
		w.rebuildClosure(facets[facet])
	}
	for _, p := range plans {
		base := p.in.baseRevisionID
		if p.outcome.Rebased {
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
// with its plan's document and summary, on base, dated by its approval.
func (c *catalogueReader) writeApprovedRevision(w *entityWriter, p plannedApproval, base *int64) {
	snapshot, err := encodeJSON(p.document)
	if err != nil {
		w.fail(err)
		return
	}
	summary := p.outcome.Summary
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

// rebuildClosure replaces the closure rows of a facet's terms, given each term's parent. A cycle or a
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
