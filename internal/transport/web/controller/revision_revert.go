package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// RevisionRevertRequest is the JSON body of a request reverting an entity to one of its approved revisions, with
// an optional summary, whose CRLF and lone CR are replaced by LF; AcknowledgedFlags are a moderator's.
type RevisionRevertRequest struct {
	Summary           *string           `json:"summary"`
	AcknowledgedFlags []domain.FlagCode `json:"acknowledged_flags"`
}

// RevisionRevert handles POST /v1/revisions/{revision_id}/revert, reverting the revision's entity to it with a new
// revision: 201 with it, pending, or approved for an interactive moderator; 200 with the revision as it is for a
// revert already made.
type RevisionRevert struct {
	Command command.Command[command.RevertRequest, command.SubmittedRevision]
}

func (c RevisionRevert) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := revisionRequestOrBadRequest(w, r, "revision_id")
	if !ok {
		return
	}
	var body RevisionRevertRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	if !flagCodesOrBadRequest(w, r, body.AcknowledgedFlags) {
		return
	}

	result, err := c.Command.Execute(r.Context(), command.RevertRequest{
		Principal: req.Principal, RevisionID: req.ID, Summary: domain.WithLineBreaksNormalized(valueOrEmpty(body.Summary)),
		AcknowledgedFlags: body.AcknowledgedFlags, Source: domain.RevisionSourceFromContext(r.Context()),
	})
	if err != nil {
		writeRevisionError(w, r, "revision", err)
		return
	}
	writeSubmitted(w, r, result)
}
