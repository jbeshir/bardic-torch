package controller

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const personsPath = "/v1/persons"

var defaultPersonQuery = datasources.PersonQuery{Sort: domain.PersonSortName,
	Page: domain.Page{Limit: domain.DefaultPageLimit}}

func TestPersonListGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	letterH := domain.Letter("H")
	wren := domain.SearchQuery{Text: "wren", Tokens: []string{"wren"}}

	cases := []struct {
		name       string
		query      string
		want       datasources.PersonQuery
		list       domain.PersonList
		listErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name: "persons",
			want: defaultPersonQuery,
			list: domain.PersonList{
				Persons: []domain.PersonSummary{
					{Slug: "hollis-fenn", Name: "Hollis Fenn", Kind: domain.PersonKindCommunityBard, IsStub: true},
					{Slug: "wren-of-the-vale", Name: testPersonName, Kind: domain.PersonKindCommunityBard},
				},
				Total:   2,
				Letters: []domain.LetterCount{{Letter: letterH, Count: 1}, {Letter: "W", Count: 1}},
			},
			wantStatus: http.StatusOK,
			wantBody: `{"data": [
					{"slug": "hollis-fenn", "name": "Hollis Fenn", "kind": "community_bard", "is_stub": true},
					{"slug": "wren-of-the-vale", "name": "Wren of the Vale", "kind": "community_bard", "is_stub": false}
				], "total": 2, "letters": [{"letter": "H", "count": 1}, {"letter": "W", "count": 1}]}`,
		},
		{
			name:       emptyListsCase,
			want:       defaultPersonQuery,
			wantStatus: http.StatusOK,
			wantBody:   emptyLetterListBody,
		},
		{
			name: everyParameterCase,
			query: "?kind=group&kind=historical&role=singer&role=poet&affiliation=northmarch&q=wren&letter=H" +
				"&limit=100&offset=10000",
			want: datasources.PersonQuery{
				Kinds:           []domain.PersonKind{domain.PersonKindGroup, domain.PersonKindHistorical},
				Roles:           []domain.PersonRole{domain.PersonRoleSinger, domain.PersonRolePoet},
				AffiliationSlug: new(testAffiliationSlug), Search: &wren, Letter: &letterH,
				Sort: domain.PersonSortRelevance, Page: widestPage,
			},
			wantStatus: http.StatusOK,
			wantBody:   emptyLetterListBody,
		},
		{
			name:       "name_order_with_search",
			query:      "?q=wren&sort=name",
			want:       datasources.PersonQuery{Search: &wren, Sort: domain.PersonSortName, Page: defaultPersonQuery.Page},
			wantStatus: http.StatusOK,
			wantBody:   emptyLetterListBody,
		},
		{
			name:  "unknown_affiliation",
			query: "?affiliation=southmarch",
			want: datasources.PersonQuery{AffiliationSlug: new("southmarch"), Sort: domain.PersonSortName,
				Page: defaultPersonQuery.Page},
			listErr:    fmt.Errorf("listing: %w", &datasources.UnknownTermError{Slug: "southmarch"}),
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code": "invalid_request", "message": "unknown affiliation: southmarch"}`,
		},
		{
			name:       listFailureCase,
			want:       defaultPersonQuery,
			listErr:    errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
		{
			name:       "timeout",
			want:       defaultPersonQuery,
			listErr:    errTimeout,
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lister := mocks.NewPublicPersonLister(t)
			lister.EXPECT().ListPublicPersons(mock.Anything, tc.want).Return(tc.list, tc.listErr)

			rec := serveGet(t, PersonListGet{Lister: lister}, personsPath+tc.query, nil, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	invalid := append([]invalidQueryCase{
		{name: unknownKindCase, query: "kind=bard",
			wantBody: invalidKindBody},
		{name: duplicateKindsCase, query: "kind=group&kind=group",
			wantBody: duplicateKindBody},
		{name: "unknown_role", query: "role=juggler",
			wantBody: `{"code":"invalid_request","message":"invalid role: not one of its allowed values"}`},
		{name: "duplicate_roles", query: "role=poet&role=singer&role=poet",
			wantBody: `{"code":"invalid_request","message":"query parameter role must not repeat a value"}`},
		{name: "affiliation_not_slug", query: "affiliation=North+March",
			wantBody: `{"code":"invalid_request","message":"invalid affiliation: must be lowercase ASCII letters and ` +
				`digits, in runs joined by single hyphens, at most 191 characters"}`},
		{name: "affiliation_repeated", query: "affiliation=northmarch&affiliation=eastmarch",
			wantBody: `{"code":"invalid_request","message":"query parameter affiliation must be given at most once"}`},
		{name: "relevance_without_search", query: "sort=relevance",
			wantBody: `{"code":"invalid_request","message":"invalid sort: relevance needs a search query q"}`},
		{name: "unknown_sort", query: "sort=title",
			wantBody: `{"code":"invalid_request","message":"invalid sort: not one of its allowed values"}`},
	}, append(invalidSearchCases, invalidPageCases...)...)
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := serveGet(t, PersonListGet{Lister: mocks.NewPublicPersonLister(t)}, personsPath+"?"+tc.query, nil,
				true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}

const emptyLetterListBody = `{"data": [], "total": 0, "letters": []}`
