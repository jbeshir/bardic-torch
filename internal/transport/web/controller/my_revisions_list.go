package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// MyRevisionList is the JSON representation of one page of the caller's own revisions, with how many there are.
type MyRevisionList struct {
	Data  []MyRevisionSummary `json:"data"`
	Total int64               `json:"total"`
}

// MyRevisionsList handles GET /v1/me/revisions, one page of the caller's own revisions, drafts included, newest
// first, optionally of one status and entity type.
type MyRevisionsList struct {
	Command command.Command[command.ListMyRevisionsRequest, domain.MyRevisionList]
}

func (c MyRevisionsList) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	status, ok := enumOrBadRequest(w, r, query, "status", domain.ParseRevisionStatus)
	if !ok {
		return
	}
	entityType, ok := enumOrBadRequest(w, r, query, "entity_type", domain.ParseEntityType)
	if !ok {
		return
	}
	page, ok := pageOrBadRequest(w, r, query)
	if !ok {
		return
	}

	list, err := c.Command.Execute(r.Context(), command.ListMyRevisionsRequest{
		Principal: principal,
		Query:     datasources.MyRevisionQuery{Status: status, EntityType: entityType, Page: page},
	})
	if err != nil {
		writeInternalError(w, r, "unable to list revisions", err)
		return
	}
	httpjson.WriteJSON(r.Context(), w, http.StatusOK, MyRevisionList{
		Data: mapSlice(list.Revisions, myRevisionSummaryFromDomain), Total: list.Total,
	})
}
