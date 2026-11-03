package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
)

// DraftDelete handles DELETE /v1/drafts/{draft_id}, discarding one of the caller's drafts.
type DraftDelete struct {
	Command command.Command[command.RevisionRequest, command.Empty]
}

func (c DraftDelete) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := revisionRequestOrBadRequest(w, r, "draft_id")
	if !ok {
		return
	}

	if _, err := c.Command.Execute(r.Context(), req); err != nil {
		writeRevisionError(w, r, "draft", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
