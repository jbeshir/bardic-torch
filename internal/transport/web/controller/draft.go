package controller

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// Draft is the JSON representation of a draft, as its author saved it.
type Draft struct {
	ID         int64             `json:"id"`
	EntityType domain.EntityType `json:"entity_type"`
	// Entity is nil for a draft of a new entity.
	Entity         *EditorEntityRef         `json:"entity"`
	BaseRevisionID *int64                   `json:"base_revision_id"`
	Document       domain.AnyEditorDocument `json:"document"`
	Summary        string                   `json:"summary"`
	CreatedAt      time.Time                `json:"created_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
	Duplicates     []DuplicateMatch         `json:"duplicates"`
	References     []EditorReference        `json:"references"`
	// Preview is the snapshot view of the draft once approved, or nil if it has none.
	Preview any `json:"preview"`
}

// DuplicateMatch is the JSON representation of an entity a new one may duplicate: its address, its name, and
// which of its names matched.
type DuplicateMatch struct {
	EntityType   domain.EntityType     `json:"entity_type"`
	Slug         *string               `json:"slug"`
	CollectionID *int64                `json:"collection_id"`
	Name         string                `json:"name"`
	Matched      domain.DuplicateField `json:"matched"`
}

func duplicateMatchFromDomain(m domain.DuplicateMatch) DuplicateMatch {
	match := DuplicateMatch{EntityType: m.EntityType, Name: m.Name, Matched: m.Matched}
	if m.EntityType == domain.EntityTypeCollection {
		match.CollectionID = &m.CollectionID
	} else {
		match.Slug = &m.Slug
	}

	return match
}

func draftFromDomain(result command.DraftResult) (Draft, error) {
	d := result.Draft
	doc, err := editorDocumentFromDomain(d.Document, d.References)
	if err != nil {
		return Draft{}, err
	}
	preview, err := optionalSnapshotView(d.Preview)
	if err != nil {
		return Draft{}, err
	}

	return Draft{
		ID: d.ID, EntityType: d.EntityType, Entity: optional(d.Entity, editorEntityRefFromDomain),
		BaseRevisionID: d.BaseRevisionID, Document: doc, Summary: d.Summary, CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt, Duplicates: mapSlice(result.Duplicates, duplicateMatchFromDomain),
		References: mapSlice(d.References, editorReferenceFromDomain), Preview: preview,
	}, nil
}

// writeDraft writes result as the response with status, or the error converting it.
func writeDraft(w http.ResponseWriter, r *http.Request, status int, result command.DraftResult) {
	body, err := draftFromDomain(result)
	if err != nil {
		WriteInternalError(w, r, "unable to show draft", err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, status, body)
}

// DraftUpdateRequest is the JSON body of a request replacing a draft's document and summary: Document is in the
// editor format of EntityType, which must be the draft's. Summary's CRLF and lone CR are replaced by LF; the
// document's text is taken as sent.
type DraftUpdateRequest struct {
	EntityType domain.EntityType `json:"entity_type"`
	Document   json.RawMessage   `json:"document"`
	Summary    string            `json:"summary"`
}

// documentOrBadRequest decodes the request's document. If it is invalid it writes 400 invalid_request and returns
// false; the caller should then return without writing.
func (d DraftUpdateRequest) documentOrBadRequest(
	w http.ResponseWriter, r *http.Request,
) (domain.AnyEditorDocument, bool) {
	return editorDocumentToDomain(r.Context(), w, d.EntityType, d.Document)
}
