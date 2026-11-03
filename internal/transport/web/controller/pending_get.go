package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// PendingGet handles GET /v1/{pieces,persons,terms,events}/{slug}/pending and
// /v1/collections/{collection_id}/pending, the caller's pending revision of the entity of EntityType, as they see
// it (D6). A placeholder slug addresses the caller's own pending new entity. Having none is not found, as is an
// entity that is neither public nor the caller's own pending new entity.
type PendingGet struct {
	Command    command.Command[command.EntityRequest, domain.MyRevision]
	EntityType domain.EntityType
}

func (c PendingGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := entityRequestOrBadRequest(w, r, c.EntityType)
	if !ok {
		return
	}

	revision, err := c.Command.Execute(r.Context(), req)
	if err != nil {
		writeRevisionError(w, r, "pending revision", err)
		return
	}
	writeMyRevision(w, r, http.StatusOK, revision)
}
