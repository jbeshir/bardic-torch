package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// RevisionSubmitRequest is the JSON body of a request submitting a draft. AcknowledgeDuplicates confirms that a new
// entity duplicates none of the entities its draft's duplicates list; AcknowledgedFlags are a moderator's.
type RevisionSubmitRequest struct {
	DraftID               int64             `json:"draft_id"`
	AcknowledgeDuplicates bool              `json:"acknowledge_duplicates"`
	AcknowledgedFlags     []domain.FlagCode `json:"acknowledged_flags"`
}

// RevisionSubmit handles POST /v1/revisions, submitting one of the caller's drafts: 201 with the revision it
// becomes, pending, or approved for an interactive moderator; 200 with the revision as it is for a draft already
// submitted.
type RevisionSubmit struct {
	Command command.Command[command.SubmitRevisionRequest, command.SubmittedRevision]
}

func (c RevisionSubmit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	var body RevisionSubmitRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	if body.DraftID < 1 {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"invalid draft_id: must be a positive integer")
		return
	}
	if !flagCodesOrBadRequest(w, r, body.AcknowledgedFlags) {
		return
	}

	result, err := c.Command.Execute(r.Context(), command.SubmitRevisionRequest{
		Principal: principal, DraftID: body.DraftID, AcknowledgeDuplicates: body.AcknowledgeDuplicates,
		AcknowledgedFlags: body.AcknowledgedFlags,
	})
	if err != nil {
		writeRevisionError(w, r, "draft", err)
		return
	}
	writeSubmitted(w, r, result)
}

// flagCodesOrBadRequest checks that every code is a flag code. If one is not, it writes 400 invalid_request and
// returns false; the caller should then return without writing.
func flagCodesOrBadRequest(w http.ResponseWriter, r *http.Request, codes []domain.FlagCode) bool {
	for _, code := range codes {
		if !code.Valid() {
			httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
				"invalid acknowledged_flags: not one of its allowed values")
			return false
		}
	}

	return true
}

// writeSubmitted writes the revision a submission or revert left: 201, or 200 for a replay.
func writeSubmitted(w http.ResponseWriter, r *http.Request, result command.SubmittedRevision) {
	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeMyRevision(w, r, status, result.Revision)
}
