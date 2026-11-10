package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// CollectionList is the JSON representation of one page of the public collections.
type CollectionList struct {
	Data    []CollectionSummary `json:"data"`
	Total   int64               `json:"total"`
	Letters []LetterCount       `json:"letters"`
}

// CollectionListGet handles GET /v1/collections, one page of the public collections, of some kinds or all, by
// sort name.
type CollectionListGet struct {
	Catalogue datasources.Catalogue
}

func (c CollectionListGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return
	}
	var q datasources.CollectionQuery
	if q.Kinds, ok = enumListOrBadRequest(w, r, query, "kind", domain.ParseCollectionKind); !ok {
		return
	}
	if q.Letter, ok = enumOrBadRequest(w, r, query, "letter", domain.ParseLetter); !ok {
		return
	}
	if q.Page, ok = pageOrBadRequest(w, r, query); !ok {
		return
	}

	list, err := c.Catalogue.ListPublicCollections(ctx, q)
	if err != nil {
		writeSearchError(w, r, "unable to list collections", err)
		return
	}

	httpjson.WriteJSON(ctx, w, http.StatusOK, CollectionList{
		Data:    mapSlice(list.Collections, collectionSummaryFromDomain),
		Total:   list.Total,
		Letters: mapSlice(list.Letters, letterCountFromDomain),
	})
}
