package controller

import (
	"errors"
	"net/http"
	"slices"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// PieceList is the JSON representation of one page of a piece search.
type PieceList struct {
	Data   []PieceSummary `json:"data"`
	Total  int64          `json:"total"`
	Facets PieceFacets    `json:"facets"`
}

// PieceFacets is the JSON representation of a piece search's facet counts.
type PieceFacets struct {
	Terms                []TermCount               `json:"terms"`
	Kinds                []PieceKindCount          `json:"kinds"`
	ConservationStatuses []ConservationStatusCount `json:"conservation_statuses"`
	Letters              []LetterCount             `json:"letters"`
}

// TermCount is the JSON representation of how many pieces of a search a term classifies.
type TermCount struct {
	Slug  string `json:"slug"`
	Count int64  `json:"count"`
}

// PieceKindCount is the JSON representation of how many pieces of a search are of a kind.
type PieceKindCount struct {
	Kind  domain.PieceKind `json:"kind"`
	Count int64            `json:"count"`
}

// ConservationStatusCount is the JSON representation of how many pieces of a search have a conservation status.
type ConservationStatusCount struct {
	ConservationStatus domain.ConservationStatus `json:"conservation_status"`
	Count              int64                     `json:"count"`
}

// LetterCount is the JSON representation of how many entities of a list sort under a letter.
type LetterCount struct {
	Letter domain.Letter `json:"letter"`
	Count  int64         `json:"count"`
}

// PieceListGet handles GET /v1/pieces, a faceted search of the public pieces.
type PieceListGet struct {
	Catalogue datasources.Catalogue
}

func (c PieceListGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query, ok := c.pieceQuery(w, r)
	if !ok {
		return
	}

	list, err := c.Catalogue.ListPublicPieces(ctx, query)
	var unknown *datasources.UnknownTermError
	switch {
	case errors.As(err, &unknown):
		// The slug is the client's own, checked to be a slug; unknown and unapproved terms give the same body.
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest, "unknown term: "+unknown.Slug)
		return
	case err != nil:
		writeSearchError(w, r, "unable to list pieces", err)
		return
	}

	httpjson.WriteJSON(ctx, w, http.StatusOK, PieceList{
		Data:  mapSlice(list.Pieces, pieceSummaryFromDomain),
		Total: list.Total,
		Facets: PieceFacets{
			Terms: mapSlice(list.Facets.Terms, func(t domain.TermCount) TermCount {
				return TermCount{Slug: t.Slug, Count: t.Count}
			}),
			Kinds: mapSlice(list.Facets.Kinds, func(k domain.PieceKindCount) PieceKindCount {
				return PieceKindCount{Kind: k.Kind, Count: k.Count}
			}),
			ConservationStatuses: mapSlice(list.Facets.ConservationStatuses,
				func(s domain.ConservationStatusCount) ConservationStatusCount {
					return ConservationStatusCount{ConservationStatus: s.ConservationStatus, Count: s.Count}
				}),
			Letters: mapSlice(list.Facets.Letters, letterCountFromDomain),
		},
	})
}

// pieceQuery returns the search the request's query parameters ask for. If they are invalid it writes 400
// invalid_request and returns false; the caller should then return without writing.
func (PieceListGet) pieceQuery(w http.ResponseWriter, r *http.Request) (datasources.PieceQuery, bool) {
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return datasources.PieceQuery{}, false
	}

	var q datasources.PieceQuery
	terms, ok := listOrBadRequest(w, r, query, "term", datasources.MaxPieceQueryTerms, slugValue, slugRule)
	if !ok {
		return datasources.PieceQuery{}, false
	}
	// Repeated terms are one filter; order is kept, so an unknown term is reported as the first one given.
	for _, term := range terms {
		if !slices.Contains(q.TermSlugs, term) {
			q.TermSlugs = append(q.TermSlugs, term)
		}
	}
	if q.Kinds, ok = enumListOrBadRequest(w, r, query, "kind", domain.ParsePieceKind); !ok {
		return datasources.PieceQuery{}, false
	}
	if q.ConservationStatuses, ok = enumListOrBadRequest(w, r, query, "conservation_status",
		domain.ParseConservationStatus); !ok {
		return datasources.PieceQuery{}, false
	}
	if q.Stub, ok = boolOrBadRequest(w, r, query, "stub"); !ok {
		return datasources.PieceQuery{}, false
	}
	if q.Search, ok = searchOrBadRequest(w, r, query); !ok {
		return datasources.PieceQuery{}, false
	}
	if q.Letter, ok = enumOrBadRequest(w, r, query, "letter", domain.ParseLetter); !ok {
		return datasources.PieceQuery{}, false
	}
	if q.Sort, ok = sortOrBadRequest(w, r, query, q.Search, domain.ParsePieceSort, domain.PieceSortTitle,
		domain.PieceSortRelevance); !ok {
		return datasources.PieceQuery{}, false
	}
	if q.Page, ok = pageOrBadRequest(w, r, query); !ok {
		return datasources.PieceQuery{}, false
	}

	return q, true
}

func letterCountFromDomain(l domain.LetterCount) LetterCount {
	return LetterCount{Letter: l.Letter, Count: l.Count}
}
