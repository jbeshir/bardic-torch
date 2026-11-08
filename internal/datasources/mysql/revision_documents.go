package mysql

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// documentKind is what the revision reads and writes do with the documents of one entity type, whose type is
// known only at run time: each method asserts its documents are of the kind's type. id is the entity's ID, 0 for a
// new entity.
type documentKind interface {
	decode(data []byte) (domain.AnyDocument, error)
	addReferences(id int64, doc domain.AnyDocument, refs *domain.SnapshotReferences)
	// editable is the projection of doc the caller may edit, with the keys of what it shows.
	editable(id int64, doc domain.AnyDocument, vis domain.EditorVisibility) (domain.AnyDocument, domain.ElementKeySet)
	// author is what the author of the submitted document doc, who received the keys received, is shown of it.
	author(id int64, doc domain.AnyDocument, received domain.ElementKeySet, vis domain.EditorVisibility) domain.AnyDocument
	// authorView is the author's view of the submitted document doc.
	authorView(id int64, doc domain.AnyDocument, received domain.ElementKeySet, vis domain.EditorVisibility) (
		domain.Snapshot, error)
	// projectedView is the same construction over a base document doc, of which the author received the
	// projection: nothing of it was typed.
	projectedView(id int64, doc domain.AnyDocument, vis domain.EditorVisibility) (domain.Snapshot, error)
	// view is the public view of doc.
	view(id int64, doc domain.AnyDocument, vis domain.SnapshotVisibility) (domain.Snapshot, error)
	// planSubmit plans a contributor's submission (domain.PlanSubmit): the full document to store and its flags.
	planSubmit(s submitState) (domain.AnyDocument, []domain.Flag, error)
	// planApproval plans an approval (domain.PlanApproval).
	planApproval(s approvalState, opts domain.ApprovalOptions) (approvalPlan, error)
	// planRebase plans a rebase (domain.PlanRebase): the document to store.
	planRebase(s rebaseState) (domain.AnyDocument, error)
	// planRevert plans a revert (domain.PlanRevert).
	planRevert(s revertState) (revertPlan, error)
	// approvalFlags are the flags approving a revision gates on (domain.ApprovalFlags).
	approvalFlags(s approvalState) []domain.Flag
	// merge merges ours, a revision, and theirs, the current document, both edits of base (domain.MergeDocuments).
	merge(base, ours, theirs domain.AnyDocument) (domain.AnyDocument, []domain.RebaseConflict)
	// changedPaths are what doc changes from base, or from the default document for a nil base
	// (domain.DiffDocuments).
	changedPaths(base, doc domain.AnyDocument) []domain.FieldPath
}

// submitState is domain.SubmitState for a document of a kind known at run time; base is nil for a new entity.
type submitState struct {
	id            int64
	submitted     domain.AnyDocument
	base          domain.AnyDocument
	received      domain.ElementKeySet
	vis           domain.EditorVisibility
	refs          domain.ReferenceIndex
	hiddenAliases []string
	summary       string
	today         domain.Date
}

// editorDocument is a document type, which is also an AnyDocument.
type editorDocument interface {
	domain.Document
	domain.AnyDocument
}

// kindOf is the documentKind of documents of type D, whose views are of type S.
type kindOf[D editorDocument, S domain.Snapshot] struct {
	references func(id int64, d D, refs *domain.SnapshotReferences)
	newView    func(id int64, d D, vis domain.SnapshotVisibility) (S, error)
}

func (k kindOf[D, S]) decode(data []byte) (domain.AnyDocument, error) {
	doc, err := domain.DecodeDocument[D](data)
	if err != nil {
		return nil, err
	}

	return doc, nil
}

func (k kindOf[D, S]) addReferences(id int64, doc domain.AnyDocument, refs *domain.SnapshotReferences) {
	k.references(id, doc.(D), refs)
}

func (k kindOf[D, S]) editable(id int64, doc domain.AnyDocument, vis domain.EditorVisibility) (
	domain.AnyDocument, domain.ElementKeySet,
) {
	projection, shown := domain.EditableDocument(id, doc.(D), vis)

	return projection, shown
}

func (k kindOf[D, S]) author(
	id int64, doc domain.AnyDocument, received domain.ElementKeySet, vis domain.EditorVisibility,
) domain.AnyDocument {
	return domain.AuthorDocument(id, doc.(D), received, vis)
}

func (k kindOf[D, S]) authorView(
	id int64, doc domain.AnyDocument, received domain.ElementKeySet, vis domain.EditorVisibility,
) (domain.Snapshot, error) {
	authorDoc := domain.AuthorDocument(id, doc.(D), received, vis)

	return k.viewOf(id, authorDoc, domain.AuthorVisibility(authorDoc, received, vis.SnapshotVisibility,
		vis.OwnPending, id))
}

func (k kindOf[D, S]) projectedView(id int64, doc domain.AnyDocument, vis domain.EditorVisibility) (
	domain.Snapshot, error,
) {
	projection, shown := domain.EditableDocument(id, doc.(D), vis)

	return k.viewOf(id, projection, domain.AuthorVisibility(projection, shown, vis.SnapshotVisibility,
		vis.OwnPending, id))
}

func (k kindOf[D, S]) view(id int64, doc domain.AnyDocument, vis domain.SnapshotVisibility) (domain.Snapshot, error) {
	return k.viewOf(id, doc.(D), vis)
}

// viewOf is newView as a domain.Snapshot.
func (k kindOf[D, S]) viewOf(id int64, d D, vis domain.SnapshotVisibility) (domain.Snapshot, error) {
	view, err := k.newView(id, d, vis)
	if err != nil {
		return nil, err
	}

	return view, nil
}

func (k kindOf[D, S]) planSubmit(s submitState) (domain.AnyDocument, []domain.Flag, error) {
	state := domain.SubmitState[D]{ID: s.id, Submitted: s.submitted.(D), Received: s.received, Visibility: s.vis,
		Refs: s.refs, HiddenAliases: s.hiddenAliases, Summary: s.summary, Today: s.today}
	if s.base != nil {
		base := s.base.(D)
		state.Base = &base
	}
	plan, err := domain.PlanSubmit(state)
	if err != nil {
		return nil, nil, err
	}

	return plan.Document, plan.Flags, nil
}

// documentKinds is the documentKind of each entity type.
var documentKinds = map[domain.EntityType]documentKind{
	domain.EntityTypePiece: kindOf[domain.PieceDocument, domain.PieceSnapshot]{
		references: func(_ int64, d domain.PieceDocument, refs *domain.SnapshotReferences) {
			pieceSnapshots.addReferences(d, refs)
		},
		newView: func(_ int64, d domain.PieceDocument, vis domain.SnapshotVisibility) (domain.PieceSnapshot, error) {
			return pieceSnapshots.view(d, vis)
		},
	},
	domain.EntityTypePerson: kindOf[domain.PersonDocument, domain.PersonSnapshot]{
		references: func(id int64, d domain.PersonDocument, refs *domain.SnapshotReferences) {
			d.AddReferences(id, refs)
		},
		newView: authorPersonView,
	},
	domain.EntityTypeTerm: kindOf[domain.TermDocument, domain.TermSnapshot]{
		references: func(_ int64, d domain.TermDocument, refs *domain.SnapshotReferences) {
			termSnapshots.addReferences(d, refs)
		},
		newView: func(_ int64, d domain.TermDocument, vis domain.SnapshotVisibility) (domain.TermSnapshot, error) {
			return termSnapshots.view(d, vis)
		},
	},
	domain.EntityTypeCollection: kindOf[domain.CollectionDocument, domain.CollectionSnapshot]{
		references: func(_ int64, d domain.CollectionDocument, refs *domain.SnapshotReferences) {
			collectionSnapshots.addReferences(d, refs)
		},
		newView: func(
			_ int64, d domain.CollectionDocument, vis domain.SnapshotVisibility,
		) (domain.CollectionSnapshot, error) {
			return collectionSnapshots.view(d, vis)
		},
	},
	domain.EntityTypeEvent: kindOf[domain.EventDocument, domain.EventSnapshot]{
		references: func(int64, domain.EventDocument, *domain.SnapshotReferences) {},
		newView: func(_ int64, d domain.EventDocument, vis domain.SnapshotVisibility) (domain.EventSnapshot, error) {
			return eventSnapshots.view(d, vis)
		},
	},
}

// authorPersonView is the view of a person document in a revision's own views. A person who is not public is a
// new person of the revision's author (their draft, or their pending, rejected or withdrawn new person), whom the
// view shows by the name their document gives them, since they have no current name.
func authorPersonView(id int64, d domain.PersonDocument, vis domain.SnapshotVisibility) (domain.PersonSnapshot, error) {
	_, public := vis.Persons[id]
	_, noPage := vis.NoPagePersons[id]
	if !public && !noPage {
		persons := make(map[int64]domain.PersonSummary, len(vis.Persons)+1)
		maps.Copy(persons, vis.Persons)
		persons[id] = domain.PersonSummary{Name: d.Name, Kind: d.Kind, IsStub: domain.PersonIsStub(d.IsStub, d.Status)}
		vis.Persons = persons
	}

	return domain.NewPersonSnapshot(id, d, vis)
}

// kindFor returns the documentKind of entityType.
func kindFor(entityType domain.EntityType) (documentKind, error) {
	kind, ok := documentKinds[entityType]
	if !ok {
		return nil, fmt.Errorf("%w [%s] of entity type", domain.ErrUnknownEnumValue, entityType)
	}

	return kind, nil
}

// encodeJSON encodes a value stored in a JSON column: a document, flags or element keys.
func encodeJSON(v any) (json.RawMessage, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encoding JSON column: %w", err)
	}

	return data, nil
}

// encodeFlags encodes a revision's flags for its JSON column; no flags are an empty list, never null.
func encodeFlags(flags []domain.Flag) (json.RawMessage, error) {
	if flags == nil {
		flags = []domain.Flag{}
	}

	return encodeJSON(flags)
}

// decodeJSONColumn decodes the JSON column what, which holds a T: no data is the zero value of T, and a list or
// map is never nil.
func decodeJSONColumn[T any](what string, data []byte) (T, error) {
	var v T
	if len(data) > 0 {
		if err := json.Unmarshal(data, &v); err != nil {
			var zero T

			return zero, fmt.Errorf("decoding %s: %w", what, err)
		}
	}
	rv := reflect.ValueOf(&v).Elem()
	switch {
	case rv.Kind() == reflect.Slice && rv.IsNil():
		rv.Set(reflect.MakeSlice(rv.Type(), 0, 0))
	case rv.Kind() == reflect.Map && rv.IsNil():
		rv.Set(reflect.MakeMap(rv.Type()))
	}

	return v, nil
}

// decodeRevision returns the documentKind of the revisions of entityType and the document in snapshot, the stored
// snapshot of one.
func decodeRevision(entityType domain.EntityType, snapshot []byte) (documentKind, domain.AnyDocument, error) {
	kind, err := kindFor(entityType)
	if err != nil {
		return nil, nil, err
	}
	doc, err := kind.decode(snapshot)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot: %w", err)
	}

	return kind, doc, nil
}

// approvalState is domain.ApprovalState for documents of a kind known at run time; base and current are nil for
// none.
type approvalState struct {
	id                                int64
	revision, base, current           domain.AnyDocument
	baseRevisionID, currentRevisionID *int64
	author                            domain.Role
	refs                              domain.ReferenceIndex
	hiddenAliases                     []string
	hiddenLinkURLs                    map[string]bool
	summary                           string
	storedFlags                       []domain.Flag
	pendingReferences                 []domain.PendingReference
	possibleDuplicate                 bool
	duplicateBlocked                  bool
	today                             domain.Date
}

// approvalPlan is domain.ApprovalPlan for a document of a kind known at run time.
type approvalPlan struct {
	document           domain.AnyDocument
	rebased            bool
	summary            string
	conservationChange *domain.ConservationChange
	termFacetRebuild   *domain.Facet
}

// rebaseState is domain.RebaseState for documents of a kind known at run time.
type rebaseState struct {
	id                 int64
	resolved, revision domain.AnyDocument
	// current is nil for a new entity.
	current domain.AnyDocument
	author  domain.Role
	vis     domain.EditorVisibility
	refs    domain.ReferenceIndex
	summary string
	today   domain.Date
}

// revertState is domain.RevertState for documents of a kind known at run time.
type revertState struct {
	id              int64
	target, current domain.AnyDocument
	vis             domain.EditorVisibility
	refs            domain.ReferenceIndex
	hiddenAliases   []string
	summary         string
	today           domain.Date
}

// revertPlan is domain.RevertPlan for documents of a kind known at run time.
type revertPlan struct {
	document, submitted domain.AnyDocument
	received            domain.ElementKeySet
	flags               []domain.Flag
}

// optional returns doc as a *D, nil for none.
func optional[D editorDocument](doc domain.AnyDocument) *D {
	if doc == nil {
		return nil
	}
	d := doc.(D)

	return &d
}

// approvalStateOf is s as a domain.ApprovalState of documents of type D.
func approvalStateOf[D editorDocument](s approvalState) domain.ApprovalState[D] {
	return domain.ApprovalState[D]{
		ID: s.id, Revision: s.revision.(D), Base: optional[D](s.base), Current: optional[D](s.current),
		BaseRevisionID: s.baseRevisionID, CurrentRevisionID: s.currentRevisionID, Author: s.author, Refs: s.refs,
		HiddenAliases: s.hiddenAliases, HiddenLinkURLs: s.hiddenLinkURLs, Summary: s.summary,
		StoredFlags: s.storedFlags, PendingReferences: s.pendingReferences, PossibleDuplicate: s.possibleDuplicate,
		DuplicateBlocked: s.duplicateBlocked, Today: s.today,
	}
}

func (k kindOf[D, S]) approvalFlags(s approvalState) []domain.Flag {
	return domain.ApprovalFlags(approvalStateOf[D](s))
}

func (k kindOf[D, S]) merge(base, ours, theirs domain.AnyDocument) (domain.AnyDocument, []domain.RebaseConflict) {
	merged, conflicts := domain.MergeDocuments(base.(D), ours.(D), theirs.(D))

	return merged, conflicts
}

func (k kindOf[D, S]) changedPaths(base, doc domain.AnyDocument) []domain.FieldPath {
	from := domain.DefaultDocument[D]()
	if base != nil {
		from = base.(D)
	}

	return domain.DiffDocuments(from, doc.(D))
}

func (k kindOf[D, S]) planApproval(s approvalState, opts domain.ApprovalOptions) (approvalPlan, error) {
	plan, err := domain.PlanApproval(approvalStateOf[D](s), opts)
	if err != nil {
		return approvalPlan{}, err
	}

	return approvalPlan{document: plan.Document, rebased: plan.Rebased, summary: plan.Summary,
		conservationChange: plan.ConservationChange, termFacetRebuild: plan.TermFacetRebuild}, nil
}

func (k kindOf[D, S]) planRebase(s rebaseState) (domain.AnyDocument, error) {
	doc, err := domain.PlanRebase(domain.RebaseState[D]{ID: s.id, Resolved: s.resolved.(D), Revision: s.revision.(D),
		Current: optional[D](s.current),
		Author:  s.author, Visibility: s.vis, Refs: s.refs, Summary: s.summary, Today: s.today})
	if err != nil {
		return nil, err
	}

	return doc, nil
}

func (k kindOf[D, S]) planRevert(s revertState) (revertPlan, error) {
	plan, err := domain.PlanRevert(domain.RevertState[D]{ID: s.id, Target: s.target.(D), Current: s.current.(D),
		Visibility: s.vis, Refs: s.refs, HiddenAliases: s.hiddenAliases, Summary: s.summary, Today: s.today})
	if err != nil {
		return revertPlan{}, err
	}

	return revertPlan{document: plan.Document, submitted: plan.Submitted, received: plan.Received,
		flags: plan.Flags}, nil
}
