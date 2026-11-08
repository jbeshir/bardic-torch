package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// Editor-format documents are sent and received as the domain's Editor*Document types, whose JSON names are
// the editor format's. An object carrying a document has its entity_type beside it, which says which of the five
// shapes the document has.

// EditorEntityRef is the JSON representation of the entity a revision or draft edits, as its author or a
// moderator addresses it: a piece, person, term or event by slug, a collection by ID. IsNew is set for a pending
// new entity, whose slug is a placeholder.
type EditorEntityRef struct {
	Slug         *string `json:"slug"`
	CollectionID *int64  `json:"collection_id"`
	IsNew        bool    `json:"is_new"`
}

// EditorReference is the JSON representation of an entity a document references, as the caller sees it.
type EditorReference struct {
	EntityType   domain.EntityType `json:"entity_type"`
	Slug         *string           `json:"slug"`
	CollectionID *int64            `json:"collection_id"`
	Label        string            `json:"label"`
	IsPending    bool              `json:"is_pending"`
}

// editorDocumentToDomain decodes raw, the field document of a request, as an editor document of entityType. If it
// is not one, it writes 400 invalid_request and returns false; the caller should then return without writing.
func editorDocumentToDomain(
	ctx context.Context, w http.ResponseWriter, entityType domain.EntityType, raw json.RawMessage,
) (domain.AnyEditorDocument, bool) {
	switch entityType {
	case domain.EntityTypePiece:
		return decodeEditorDocument[domain.EditorPieceDocument](ctx, w, raw)
	case domain.EntityTypePerson:
		return decodeEditorDocument[domain.EditorPersonDocument](ctx, w, raw)
	case domain.EntityTypeTerm:
		return decodeEditorDocument[domain.EditorTermDocument](ctx, w, raw)
	case domain.EntityTypeCollection:
		return decodeEditorDocument[domain.EditorCollectionDocument](ctx, w, raw)
	case domain.EntityTypeEvent:
		return decodeEditorDocument[domain.EditorEventDocument](ctx, w, raw)
	default:
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid entity_type: not one of its allowed values")
		return nil, false
	}
}

func decodeEditorDocument[D domain.AnyEditorDocument](
	ctx context.Context, w http.ResponseWriter, raw json.RawMessage,
) (domain.AnyEditorDocument, bool) {
	var doc D
	if !httpjson.DecodeJSONField(ctx, w, raw, "document", &doc) {
		return nil, false
	}

	return doc, true
}

// editorDocumentFromDomain returns doc in the editor format, with the slugs of refs, the references the caller
// may see.
func editorDocumentFromDomain(doc domain.AnyDocument, refs []domain.EditorReference) (domain.AnyEditorDocument, error) {
	editor, err := domain.ToEditor(doc, domain.NewSlugMap(refs))
	if err != nil {
		return nil, fmt.Errorf("converting document to the editor format: %w", err)
	}

	return editor, nil
}

func editorEntityRefFromDomain(e domain.RevisionEntity) EditorEntityRef {
	if e.Type == domain.EntityTypeCollection {
		return EditorEntityRef{CollectionID: &e.ID, IsNew: e.IsNew}
	}

	return EditorEntityRef{Slug: &e.Slug, IsNew: e.IsNew}
}

func editorReferenceFromDomain(r domain.EditorReference) EditorReference {
	ref := EditorReference{EntityType: r.EntityType, Label: r.Label, IsPending: r.IsPending}
	if r.EntityType == domain.EntityTypeCollection {
		ref.CollectionID = &r.ID
	} else {
		ref.Slug = &r.Slug
	}

	return ref
}

// entityKeyOrBadRequest returns the key of the entity of entityType the request's path addresses: by its
// collection_id for a collection, else by its slug. If the path value is invalid it writes 400 invalid_request and
// returns false; the caller should then return without writing.
func entityKeyOrBadRequest(
	w http.ResponseWriter, r *http.Request, entityType domain.EntityType,
) (domain.EntityKey, bool) {
	if entityType == domain.EntityTypeCollection {
		id, ok := idOrBadRequest(w, r, "collection_id")
		return domain.EntityKey{Type: entityType, CollectionID: id}, ok
	}
	slug, ok := slugOrBadRequest(w, r)

	return domain.EntityKey{Type: entityType, Slug: slug}, ok
}

// snapshotViews returns the JSON views of a snapshot and of its base's (nil for none), with the snapshot's
// top-level fields that differ from the base's, as a public revision shows them.
func snapshotViews(snapshot, base domain.Snapshot) (view, baseView any, changed []string, err error) {
	switch s := snapshot.(type) {
	case domain.PieceSnapshot:
		return typedSnapshotViews(s, base, pieceSnapshotFromDomain)
	case domain.PersonSnapshot:
		return typedSnapshotViews(s, base, personSnapshotFromDomain)
	case domain.TermSnapshot:
		return typedSnapshotViews(s, base, termSnapshotFromDomain)
	case domain.CollectionSnapshot:
		return typedSnapshotViews(s, base, collectionSnapshotFromDomain)
	case domain.EventSnapshot:
		return typedSnapshotViews(s, base, eventSnapshotFromDomain)
	default:
		return nil, nil, nil, fmt.Errorf("%w [%T]", errUnknownRevision, snapshot)
	}
}

func typedSnapshotViews[S domain.Snapshot, JS any](
	snapshot S, base domain.Snapshot, view func(S) JS,
) (any, any, []string, error) {
	v := view(snapshot)
	var baseJSON *JS
	if base != nil {
		typed, ok := base.(S)
		if !ok {
			return nil, nil, nil, fmt.Errorf("%w: base [%T] of [%T]", errUnknownRevision, base, snapshot)
		}
		baseJSON = new(view(typed))
	}
	changed, err := changedFields(v, baseJSON)
	if err != nil {
		return nil, nil, nil, err
	}
	if baseJSON == nil {
		return v, nil, changed, nil
	}

	return v, *baseJSON, changed, nil
}

// optionalSnapshotView returns the JSON view of snapshot, or nil for none.
func optionalSnapshotView(snapshot domain.Snapshot) (any, error) {
	if snapshot == nil {
		return nil, nil
	}
	view, _, _, err := snapshotViews(snapshot, nil)

	return view, err
}
