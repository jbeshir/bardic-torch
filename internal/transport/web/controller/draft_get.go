package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
)

// DraftGet handles GET /v1/drafts/{draft_id}, one of the caller's drafts. Another user's draft is not found,
// exactly like an unknown one; the caller's revision that is no longer a draft is 409 not_draft.
type DraftGet struct {
	Command command.Command[command.RevisionRequest, command.DraftResult]
}

func (c DraftGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := revisionRequestOrBadRequest(w, r, "draft_id")
	if !ok {
		return
	}

	result, err := c.Command.Execute(r.Context(), req)
	if err != nil {
		writeRevisionError(w, r, "draft", err)
		return
	}
	writeDraft(w, r, http.StatusOK, result)
}

// revisionRequestOrBadRequest returns the request about the caller's draft or revision with the ID in the path
// value name. If the request is anonymous it writes 401, and if the ID is invalid 400 invalid_request, and returns
// false; the caller should then return without writing.
func revisionRequestOrBadRequest(w http.ResponseWriter, r *http.Request, name string) (command.RevisionRequest, bool) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return command.RevisionRequest{}, false
	}
	id, ok := idOrBadRequest(w, r, name)

	return command.RevisionRequest{Principal: principal, ID: id}, ok
}
