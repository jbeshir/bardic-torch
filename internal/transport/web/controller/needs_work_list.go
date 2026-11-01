package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// NeedsWorkList is the JSON representation of one page of the public pieces and persons needing work.
type NeedsWorkList struct {
	Data   []NeedsWorkEntry `json:"data"`
	Total  int64            `json:"total"`
	Counts []NeedsWorkCount `json:"counts"`
}

// NeedsWorkEntry is the JSON representation of a piece or person needing work.
type NeedsWorkEntry struct {
	EntityType domain.EntityType        `json:"entity_type"`
	Slug       string                   `json:"slug"`
	Title      string                   `json:"title"`
	Reasons    []domain.NeedsWorkReason `json:"reasons"`
}

// NeedsWorkCount is the JSON representation of how many entries need work for a reason.
type NeedsWorkCount struct {
	Reason domain.NeedsWorkReason `json:"reason"`
	Count  int64                  `json:"count"`
}

// NeedsWorkListGet handles GET /v1/needs-work, one page of the public pieces and persons needing work, for one
// reason or any, with the count for every reason.
type NeedsWorkListGet struct {
	Lister datasources.PublicNeedsWorkLister
}

func (c NeedsWorkListGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	reason, ok := enumOrBadRequest(w, r, query, "reason", domain.ParseNeedsWorkReason)
	if !ok {
		return
	}
	page, ok := pageOrBadRequest(w, r, query)
	if !ok {
		return
	}

	list, err := c.Lister.ListPublicNeedsWork(ctx, reason, page)
	if err != nil {
		writeSearchError(w, r, "unable to list needs-work entries", err)
		return
	}

	httpjson.WriteJSON(ctx, w, http.StatusOK, NeedsWorkList{
		Data: mapSlice(list.Entries, func(e domain.NeedsWorkEntry) NeedsWorkEntry {
			return NeedsWorkEntry{EntityType: e.EntityType, Slug: e.Slug, Title: e.Title, Reasons: nonNil(e.Reasons)}
		}),
		Total: list.Total,
		Counts: mapSlice(list.Counts, func(n domain.NeedsWorkCount) NeedsWorkCount {
			return NeedsWorkCount{Reason: n.Reason, Count: n.Count}
		}),
	})
}
