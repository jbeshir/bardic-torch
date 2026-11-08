package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// DraftUpdate handles PUT /v1/drafts/{draft_id}, replacing one of the caller's drafts' document and summary and
// keeping its base.
type DraftUpdate struct {
	Command command.Command[command.UpdateDraftRequest, command.DraftResult]
}

func (c DraftUpdate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	req, ok := revisionRequestOrBadRequest(w, r, "draft_id")
	if !ok {
		return
	}
	var body DraftUpdateRequest
	if !httpjson.DecodeJSONBodyLimit(w, r, &body, httpjson.MaxDocumentBodyBytes) {
		return
	}
	doc, sent, ok := body.documentOrBadRequest(w, r)
	if !ok {
		return
	}

	result, err := c.Command.Execute(r.Context(), command.UpdateDraftRequest{
		Principal: req.Principal, ID: req.ID, Document: doc, Sent: sent,
		Summary: domain.WithLineBreaksNormalized(body.Summary),
	})
	if err != nil {
		writeRevisionError(w, r, "draft", err)
		return
	}
	writeDraft(w, r, http.StatusOK, result)
}
