package controller

import (
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// PersonList is the JSON representation of one page of a person search.
type PersonList struct {
	Data    []PersonSummary `json:"data"`
	Total   int64           `json:"total"`
	Letters []LetterCount   `json:"letters"`
}

// PersonListGet handles GET /v1/persons, a search of the public persons. Persons who prefer no page are listed
// as stubs.
type PersonListGet struct {
	Catalogue datasources.Catalogue
}

func (c PersonListGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	query, ok := c.personQuery(w, r)
	if !ok {
		return
	}

	list, err := c.Catalogue.ListPublicPersons(ctx, query)
	var unknown *datasources.UnknownTermError
	switch {
	case errors.As(err, &unknown):
		// The slug is the client's own, checked to be a slug; unknown and unapproved terms give the same body.
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"unknown affiliation: "+unknown.Slug)
		return
	case err != nil:
		writeSearchError(w, r, "unable to list persons", err)
		return
	}

	httpjson.WriteJSON(ctx, w, http.StatusOK, PersonList{
		Data:    mapSlice(list.Persons, personSummaryFromDomain),
		Total:   list.Total,
		Letters: mapSlice(list.Letters, letterCountFromDomain),
	})
}

// personQuery returns the search the request's query parameters ask for. If they are invalid it writes 400
// invalid_request and returns false; the caller should then return without writing.
func (PersonListGet) personQuery(w http.ResponseWriter, r *http.Request) (datasources.PersonQuery, bool) {
	query, ok := queryOrBadRequest(w, r)
	if !ok {
		return datasources.PersonQuery{}, false
	}

	var q datasources.PersonQuery
	if q.Kinds, ok = enumListOrBadRequest(w, r, query, "kind", domain.ParsePersonKind); !ok {
		return datasources.PersonQuery{}, false
	}
	if q.Roles, ok = enumListOrBadRequest(w, r, query, "role", domain.ParsePersonRole); !ok {
		return datasources.PersonQuery{}, false
	}
	if q.AffiliationSlug, ok = slugParamOrBadRequest(w, r, query, "affiliation"); !ok {
		return datasources.PersonQuery{}, false
	}
	if q.Search, ok = searchOrBadRequest(w, r, query); !ok {
		return datasources.PersonQuery{}, false
	}
	if q.Letter, ok = enumOrBadRequest(w, r, query, "letter", domain.ParseLetter); !ok {
		return datasources.PersonQuery{}, false
	}
	if q.Sort, ok = sortOrBadRequest(w, r, query, q.Search, domain.ParsePersonSort, domain.PersonSortName,
		domain.PersonSortRelevance); !ok {
		return datasources.PersonQuery{}, false
	}
	if q.Page, ok = pageOrBadRequest(w, r, query); !ok {
		return datasources.PersonQuery{}, false
	}

	return q, true
}
