package mysql

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// documentKind is what the revision reads and writes do with the documents of one entity type, whose type is
// known only at run time: each method asserts its documents are of the kind's type. id is the entity's ID, 0 for a
// new entity.
type documentKind interface {
	decode(data []byte) (domain.AnyDocument, error)
	addReferences(id int64, doc domain.AnyDocument, refs *domain.SnapshotReferences)
	// editable is the projection of doc the caller may edit, with the keys of what it shows (D5).
	editable(id int64, doc domain.AnyDocument, vis domain.EditorVisibility) (domain.AnyDocument, domain.ElementKeySet)
	// author is what the author of the submitted document doc, who received the keys received, is shown of it (D6).
	author(id int64, doc domain.AnyDocument, received domain.ElementKeySet, vis domain.EditorVisibility) domain.AnyDocument
	// authorView is the author's view of the submitted document doc (D6).
	authorView(id int64, doc domain.AnyDocument, received domain.ElementKeySet, vis domain.EditorVisibility) (
		domain.Snapshot, error)
	// projectedView is the same construction over a base document doc, of which the author received the
	// projection: nothing of it was typed.
	projectedView(id int64, doc domain.AnyDocument, vis domain.EditorVisibility) (domain.Snapshot, error)
	// view is the public view of doc.
	view(id int64, doc domain.AnyDocument, vis domain.SnapshotVisibility) (domain.Snapshot, error)
	// planSubmit plans a contributor's submission (domain.PlanSubmit): the full document to store and its flags.
	planSubmit(s submitState) (domain.AnyDocument, []domain.Flag, error)
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

// decodeReceivedKeys decodes a revision's received keys; a revision with none received nothing.
func decodeReceivedKeys(data json.RawMessage) (domain.ElementKeySet, error) {
	keys := domain.ElementKeySet{}
	if len(data) == 0 {
		return keys, nil
	}
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("decoding received keys: %w", err)
	}

	return keys, nil
}
