package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// CollectionSummary is the JSON representation of a collection in a list of collections.
type CollectionSummary struct {
	ID               int64                 `json:"id"`
	Name             string                `json:"name"`
	Kind             domain.CollectionKind `json:"kind"`
	URL              *string               `json:"url"`
	IsBardicResource bool                  `json:"is_bardic_resource"`
}

// Collection is the JSON representation of a public collection's page.
type Collection struct {
	CollectionSummary

	UpdatedAt      time.Time                   `json:"updated_at"`
	DescriptionMD  *string                     `json:"description_md"`
	Enumerable     domain.CollectionEnumerable `json:"enumerable"`
	Region         *TermSummary                `json:"region"`
	Parent         *CollectionSummary          `json:"parent"`
	Children       []CollectionSummary         `json:"children"`
	Citations      []Citation                  `json:"citations"`
	CitationsTotal int64                       `json:"citations_total"`
}

// Citation is the JSON representation of a link in a collection attached to a piece or person.
type Citation struct {
	Piece   *PieceSummary     `json:"piece"`
	Person  *PersonSummary    `json:"person"`
	URL     string            `json:"url"`
	Title   *string           `json:"title"`
	Locator *string           `json:"locator"`
	Roles   []domain.LinkRole `json:"roles"`
}

// CollectionGet handles GET /v1/collections/{collection_id}, a public collection's page. An unapproved
// collection is not found, exactly like an unknown one.
type CollectionGet struct {
	Catalogue datasources.Catalogue
}

func (c CollectionGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	id, ok := idOrBadRequest(w, r, "collection_id")
	if !ok {
		return
	}

	collection, err := c.Catalogue.GetPublicCollection(ctx, id)
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, "collection not found")
	case err != nil:
		writeInternalError(w, r, "unable to get collection", err)
	default:
		httpjson.WriteJSON(ctx, w, http.StatusOK, collectionFromDomain(collection))
	}
}

func collectionSummaryFromDomain(s domain.CollectionSummary) CollectionSummary {
	return CollectionSummary{ID: s.ID, Name: s.Name, Kind: s.Kind, URL: s.URL, IsBardicResource: s.IsBardicResource}
}

func collectionFromDomain(c domain.Collection) Collection {
	return Collection{
		CollectionSummary: collectionSummaryFromDomain(c.CollectionSummary),
		UpdatedAt:         c.UpdatedAt,
		DescriptionMD:     c.DescriptionMD,
		Enumerable:        c.Enumerable,
		Region:            optional(c.Region, termSummaryFromDomain),
		Parent:            optional(c.Parent, collectionSummaryFromDomain),
		Children:          mapSlice(c.Children, collectionSummaryFromDomain),
		Citations: mapSlice(c.Citations, func(citation domain.Citation) Citation {
			return Citation{
				Piece:   optional(citation.Piece, pieceSummaryFromDomain),
				Person:  optional(citation.Person, personSummaryFromDomain),
				URL:     citation.URL,
				Title:   citation.Title,
				Locator: citation.Locator,
				Roles:   nonNil(citation.Roles),
			}
		}),
		CitationsTotal: c.CitationsTotal,
	}
}
