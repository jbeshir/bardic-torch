package controller

import (
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// RevisionHistory is the JSON representation of one page of a public entity's approved revisions.
type RevisionHistory struct {
	Data  []RevisionSummary `json:"data"`
	Total int64             `json:"total"`
}

// HistoryGet handles GET /v1/{pieces,persons,terms,events}/{slug}/history, one page of the approved revisions
// of the public entity of EntityType with the slug, newest first. An entity that is not public is not found,
// exactly like an unknown one.
type HistoryGet struct {
	Lister datasources.PublicHistoryLister
	// EntityType is the type of entity the route addresses.
	EntityType domain.SlugEntityType
}

func (c HistoryGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}
	page, ok := historyPageOrBadRequest(w, r)
	if !ok {
		return
	}

	history, err := c.Lister.ListPublicHistory(r.Context(), c.EntityType, slug, page)
	writeHistory(w, r, c.EntityType.String(), history, err)
}

// CollectionHistoryGet handles GET /v1/collections/{collection_id}/history, one page of the approved revisions of
// the public collection with the ID, newest first. A collection that is not public is not found, exactly like an
// unknown one.
type CollectionHistoryGet struct {
	Lister datasources.PublicCollectionHistoryLister
}

func (c CollectionHistoryGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := idOrBadRequest(w, r, "collection_id")
	if !ok {
		return
	}
	page, ok := historyPageOrBadRequest(w, r)
	if !ok {
		return
	}

	history, err := c.Lister.ListPublicCollectionHistory(r.Context(), id, page)
	writeHistory(w, r, string(domain.EntityTypeCollection), history, err)
}

// historyPageOrBadRequest returns the page of history the request asks for. If its query is invalid it writes
// 400 invalid_request and returns false; the caller should then return without writing.
func historyPageOrBadRequest(w http.ResponseWriter, r *http.Request) (domain.Page, bool) {
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return domain.Page{}, false
	}

	return pageOrBadRequest(w, r, query)
}

// writeHistory writes the response to a history request for an entity of type what: the history, or the error
// listing it.
func writeHistory(w http.ResponseWriter, r *http.Request, what string, history domain.RevisionHistory, err error) {
	ctx := r.Context()
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, what+" not found")
	case err != nil:
		writeInternalError(w, r, "unable to list history", err)
	default:
		httpjson.WriteJSON(ctx, w, http.StatusOK, RevisionHistory{
			Data:  mapSlice(history.Revisions, revisionSummaryFromDomain),
			Total: history.Total,
		})
	}
}
