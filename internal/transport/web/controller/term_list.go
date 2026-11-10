package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// TermList is the JSON representation of the list of public terms.
type TermList struct {
	Data []TermListItem `json:"data"`
}

// TermListItem is the JSON representation of a term in the list of terms.
type TermListItem struct {
	TermSummary

	Name        string  `json:"name"`
	ParentSlug  *string `json:"parent_slug"`
	PieceCount  int64   `json:"piece_count"`
	PersonCount int64   `json:"person_count"`
}

// TermListGet handles GET /v1/terms, the public terms of one facet or every facet, in tree order.
type TermListGet struct {
	Catalogue datasources.Catalogue
}

func (c TermListGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	facet, ok := enumOrBadRequest(w, r, query, "facet", domain.ParseFacet)
	if !ok {
		return
	}

	terms, err := c.Catalogue.ListPublicTerms(ctx, facet)
	if err != nil {
		writeInternalError(w, r, "unable to list terms", err)
		return
	}

	httpjson.WriteJSON(ctx, w, http.StatusOK, TermList{Data: mapSlice(terms, func(t domain.TermListItem) TermListItem {
		return TermListItem{
			TermSummary: termSummaryFromDomain(t.TermSummary),
			Name:        t.Name,
			ParentSlug:  t.ParentSlug,
			PieceCount:  t.PieceCount,
			PersonCount: t.PersonCount,
		}
	})})
}
