package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// EventList is the JSON representation of one page of the public events and venues.
type EventList struct {
	Data  []EventSummary `json:"data"`
	Total int64          `json:"total"`
}

// EventListGet handles GET /v1/events, one page of the public events and venues, of one kind or both, by name.
type EventListGet struct {
	Lister datasources.PublicEventLister
}

func (c EventListGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	kind, ok := enumOrBadRequest(w, r, query, "kind", domain.ParseEventKind)
	if !ok {
		return
	}
	page, ok := pageOrBadRequest(w, r, query)
	if !ok {
		return
	}

	list, err := c.Lister.ListPublicEvents(ctx, kind, page)
	if err != nil {
		writeInternalError(w, r, "unable to list events", err)
		return
	}

	httpjson.WriteJSON(ctx, w, http.StatusOK, EventList{
		Data:  mapSlice(list.Events, eventSummaryFromDomain),
		Total: list.Total,
	})
}
