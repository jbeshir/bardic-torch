package controller

import (
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// Event is the JSON representation of a public event's or venue's page.
type Event struct {
	EventSummary

	DescriptionMD     *string            `json:"description_md"`
	Performances      []EventPerformance `json:"performances"`
	PerformancesTotal int64              `json:"performances_total"`
}

// EventPerformance is the JSON representation of a performance on an event's page.
type EventPerformance struct {
	Piece               *PieceSummary   `json:"piece"`
	Link                PerformanceLink `json:"link"`
	Year                *int16          `json:"year"`
	Performers          []PersonSummary `json:"performers"`
	DescriptionVerbatim string          `json:"description_verbatim"`
}

// EventGet handles GET /v1/events/{slug}, a public event's or venue's page. An unapproved event is not found,
// exactly like an unknown one.
type EventGet struct {
	Catalogue datasources.Catalogue
}

func (c EventGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	slug, ok := slugOrBadRequest(w, r)
	if !ok {
		return
	}

	event, err := c.Catalogue.GetPublicEvent(ctx, slug)
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, "event not found")
	case err != nil:
		writeInternalError(w, r, "unable to get event", err)
	default:
		httpjson.WriteJSON(ctx, w, http.StatusOK, eventFromDomain(event))
	}
}

func eventFromDomain(e domain.Event) Event {
	return Event{
		EventSummary:  eventSummaryFromDomain(e.EventSummary),
		DescriptionMD: e.DescriptionMD,
		Performances: mapSlice(e.Performances, func(p domain.EventPerformance) EventPerformance {
			return EventPerformance{
				Piece:               optional(p.Piece, pieceSummaryFromDomain),
				Link:                performanceLinkFromDomain(p.Link),
				Year:                p.Year,
				Performers:          mapSlice(p.Performers, personSummaryFromDomain),
				DescriptionVerbatim: p.DescriptionVerbatim,
			}
		}),
		PerformancesTotal: e.PerformancesTotal,
	}
}
