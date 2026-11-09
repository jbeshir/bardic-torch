package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// DraftCreateRequest is the JSON body of a request saving a draft: of the entity Entity, or of a new entity if it is
// null, on the base BaseRevisionID, or the entity's current revision if it is null.
type DraftCreateRequest struct {
	DraftUpdateRequest

	Entity         *DraftEntity `json:"entity"`
	BaseRevisionID *int64       `json:"base_revision_id"`
}

// DraftEntity addresses the entity a draft is of: by slug, or a collection by ID.
type DraftEntity struct {
	Slug         *string `json:"slug"`
	CollectionID *int64  `json:"collection_id"`
}

// DraftCreate handles POST /v1/drafts, saving the caller's draft: 201 with a new draft, or 200 with their draft of
// the entity, which the request replaced.
type DraftCreate struct {
	Command command.Command[command.SaveDraftRequest, command.SavedDraft]
}

func (c DraftCreate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	var body DraftCreateRequest
	if !httpjson.DecodeJSONBodyLimit(w, r, &body, httpjson.MaxDocumentBodyBytes) {
		return
	}
	doc, sent, ok := body.documentOrBadRequest(w, r)
	if !ok {
		return
	}
	entity, ok := body.entityOrBadRequest(w, r)
	if !ok {
		return
	}

	saved, err := c.Command.Execute(r.Context(), command.SaveDraftRequest{
		Principal: principal, Entity: entity, BaseRevisionID: body.BaseRevisionID, Document: doc,
		Sent: sent, Summary: domain.WithLineBreaksNormalized(body.Summary),
		Source: domain.RevisionSourceFromContext(r.Context()),
	})
	if err != nil {
		writeRevisionError(w, r, "entity", err)
		return
	}

	status := http.StatusOK
	if saved.Created {
		status = http.StatusCreated
	}
	writeDraft(w, r, status, saved.DraftResult)
}

// entityOrBadRequest returns the key of the entity the request addresses, nil for a new entity. A collection is
// addressed by its collection_id, any other entity by its slug; if the address is not that, it writes 400
// invalid_request and returns false; the caller should then return without writing.
func (b DraftCreateRequest) entityOrBadRequest(w http.ResponseWriter, r *http.Request) (*domain.EntityKey, bool) {
	if b.Entity == nil {
		return nil, true
	}
	key := domain.EntityKey{Type: b.EntityType}
	switch {
	case b.EntityType == domain.EntityTypeCollection && b.Entity.Slug == nil && b.Entity.CollectionID != nil &&
		*b.Entity.CollectionID > 0:
		key.CollectionID = *b.Entity.CollectionID
	case b.EntityType != domain.EntityTypeCollection && b.Entity.CollectionID == nil && b.Entity.Slug != nil &&
		domain.IsSlug(*b.Entity.Slug):
		key.Slug = *b.Entity.Slug
	default:
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid entity: a collection is addressed by a positive collection_id, any other entity by a slug")
		return nil, false
	}

	return &key, true
}
