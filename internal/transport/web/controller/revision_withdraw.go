package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// RevisionWithdraw handles POST /v1/revisions/{revision_id}/withdraw, withdrawing one of the caller's pending
// revisions. It takes no body. Another user's revision is not found, exactly like an unknown one.
type RevisionWithdraw struct {
	Command command.Command[command.RevisionRequest, domain.MyRevision]
}

func (c RevisionWithdraw) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
