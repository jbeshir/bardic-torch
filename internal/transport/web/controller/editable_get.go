package controller

import (
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// Editable is the JSON representation of the document the caller starts an edit of an entity from (D5).
type Editable struct {
	EntityType     domain.EntityType        `json:"entity_type"`
	Entity         EditorEntityRef          `json:"entity"`
	BaseRevisionID *int64                   `json:"base_revision_id"`
	StartingPoint  domain.StartingPoint     `json:"starting_point"`
	Document       domain.AnyEditorDocument `json:"document"`
	References     []EditorReference        `json:"references"`
	LinkStates     []LinkState              `json:"link_states"`
	Draft          *DraftRef                `json:"draft"`
	Pending        *PendingRef              `json:"pending"`
}

// LinkState is the JSON representation of the moderator state of a document's link, by its URL.
type LinkState struct {
	URL      string           `json:"url"`
	LinkID   *int64           `json:"link_id"`
	IsHidden bool             `json:"is_hidden"`
	Liveness *domain.Liveness `json:"liveness"`
}

// DraftRef is the JSON representation of the caller's draft of an entity.
type DraftRef struct {
	ID        int64     `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PendingRef is the JSON representation of the caller's pending revision of an entity.
type PendingRef struct {
	ID          int64     `json:"id"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// EditableGet handles GET /v1/{pieces,persons,terms,events}/{slug}/editable and
// /v1/collections/{collection_id}/editable, the document the caller starts an edit of the entity of EntityType
// from. An entity that is neither public nor the caller's own pending new entity is not found, exactly like an
// unknown one.
type EditableGet struct {
	Command    command.Command[command.EntityRequest, domain.Editable]
	EntityType domain.EntityType
}

func (c EditableGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := entityRequestOrBadRequest(w, r, c.EntityType)
	if !ok {
		return
	}

	editable, err := c.Command.Execute(r.Context(), req)
	if err != nil {
		writeRevisionError(w, r, string(c.EntityType), err)
		return
	}
	doc, err := editorDocumentFromDomain(editable.Document, editable.References)
	if err != nil {
		WriteInternalError(w, r, "unable to show editable document", err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, Editable{
		EntityType: c.EntityType, Entity: editorEntityRefFromDomain(editable.Entity),
		BaseRevisionID: editable.BaseRevisionID, StartingPoint: editable.StartingPoint, Document: doc,
		References: mapSlice(editable.References, editorReferenceFromDomain),
		LinkStates: mapSlice(editable.LinkStates, linkStateFromDomain),
		Draft: optional(editable.Draft, func(d domain.DraftRef) DraftRef {
			return DraftRef{ID: d.ID, UpdatedAt: d.UpdatedAt}
		}),
		Pending: optional(editable.Pending, func(p domain.PendingRef) PendingRef {
			return PendingRef{ID: p.ID, SubmittedAt: p.SubmittedAt}
		}),
	})
}

// entityRequestOrBadRequest returns the request about the entity of entityType the path addresses. If the request
// is anonymous it writes 401, and if the address is invalid 400 invalid_request, and returns false; the caller
// should then return without writing.
func entityRequestOrBadRequest(
	w http.ResponseWriter, r *http.Request, entityType domain.EntityType,
) (command.EntityRequest, bool) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return command.EntityRequest{}, false
	}
	key, ok := entityKeyOrBadRequest(w, r, entityType)

	return command.EntityRequest{Principal: principal, Key: key}, ok
}
