package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// MyRevisionGet handles GET /v1/me/revisions/{revision_id}, one of the caller's own revisions with its views.
// Another user's revision is not found, exactly like an unknown one.
type MyRevisionGet struct {
	Command command.Command[command.RevisionRequest, domain.MyRevision]
}

func (c MyRevisionGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := revisionRequestOrBadRequest(w, r, "revision_id")
	if !ok {
		return
	}

	revision, err := c.Command.Execute(r.Context(), req)
	if err != nil {
		writeRevisionError(w, r, "revision", err)
		return
	}
	writeMyRevision(w, r, http.StatusOK, revision)
}
