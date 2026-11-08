package controller

import (
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// The paged lists of what an entity's page caps at 200 items, in the page's order: a page of the size of the
// page's cap, from offset 0, is the page's list.

// PersonPerformanceList is the JSON representation of one page of a public person's performances.
type PersonPerformanceList struct {
	Data  []PersonPerformance `json:"data"`
	Total int64               `json:"total"`
}

// CitationList is the JSON representation of one page of a public collection's citations.
type CitationList struct {
	Data  []Citation `json:"data"`
	Total int64      `json:"total"`
}

// EventPerformanceList is the JSON representation of one page of the performances at a public event.
type EventPerformanceList struct {
	Data  []EventPerformance `json:"data"`
	Total int64              `json:"total"`
}

// PersonPerformancesGet handles GET /v1/persons/{slug}/performances, one page of a public person's performances.
// A person who prefers no page has none, as their page shows none; an unapproved person is not found, exactly like
// an unknown one.
type PersonPerformancesGet struct {
	Lister datasources.PublicPersonPerformanceLister
}

func (c PersonPerformancesGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}
	page, ok := requestPageOrBadRequest(w, r)
	if !ok {
		return
	}

	list, err := c.Lister.ListPublicPersonPerformances(r.Context(), slug, page)
	writePagedList(w, r, "person not found", "unable to list person performances", err, func() any {
		return PersonPerformanceList{Data: mapSlice(list.Performances, personPerformanceFromDomain), Total: list.Total}
	})
}

// CollectionCitationsGet handles GET /v1/collections/{collection_id}/citations, one page of a public collection's
// citations. An unapproved collection is not found, exactly like an unknown one.
type CollectionCitationsGet struct {
	Lister datasources.PublicCollectionCitationLister
}

func (c CollectionCitationsGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	id, ok := idOrBadRequest(w, r, "collection_id")
	if !ok {
		return
	}
	page, ok := requestPageOrBadRequest(w, r)
	if !ok {
		return
	}

	list, err := c.Lister.ListPublicCollectionCitations(r.Context(), id, page)
	writePagedList(w, r, "collection not found", "unable to list collection citations", err, func() any {
		return CitationList{Data: mapSlice(list.Citations, citationFromDomain), Total: list.Total}
	})
}

// EventPerformancesGet handles GET /v1/events/{slug}/performances, one page of the performances at a public event
// or venue. An unapproved event is not found, exactly like an unknown one.
type EventPerformancesGet struct {
	Lister datasources.PublicEventPerformanceLister
}

func (c EventPerformancesGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}
	page, ok := requestPageOrBadRequest(w, r)
	if !ok {
		return
	}

	list, err := c.Lister.ListPublicEventPerformances(r.Context(), slug, page)
	writePagedList(w, r, "event not found", "unable to list event performances", err, func() any {
		return EventPerformanceList{Data: mapSlice(list.Performances, eventPerformanceFromDomain), Total: list.Total}
	})
}

// writePagedList writes the response to a request for a paged list: the list body returns, or 404 with notFound,
// or the error listing it, logged with msg.
func writePagedList(w http.ResponseWriter, r *http.Request, notFound, msg string, err error, body func() any) {
	ctx := r.Context()
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, notFound)
	case err != nil:
		WriteInternalError(w, r, msg, err)
	default:
		httpjson.WriteJSON(ctx, w, http.StatusOK, body())
	}
}
