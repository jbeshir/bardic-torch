package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const collectionsPath = "/v1/collections"

func TestCollectionListGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	defaultQuery := datasources.CollectionQuery{Page: domain.Page{Limit: domain.DefaultPageLimit}}
	letterN := domain.Letter("N")

	cases := []struct {
		name       string
		query      string
		want       datasources.CollectionQuery
		list       domain.CollectionList
		listErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name: "collections",
			want: defaultQuery,
			list: domain.CollectionList{
				Collections: []domain.CollectionSummary{
					{ID: 3, Name: "2nd Folio", Kind: domain.CollectionKindArchivedSongbook},
					{ID: 12, Name: "The Northmarch Songbook", Kind: domain.CollectionKindSongbookPDF,
						URL: new("https://example.org/songbook.pdf"), IsBardicResource: true},
				},
				Total:   2,
				Letters: []domain.LetterCount{{Letter: domain.LetterOther, Count: 1}, {Letter: letterN, Count: 1}},
			},
			wantStatus: http.StatusOK,
			wantBody: `{"data": [
					{"id": 3, "name": "2nd Folio", "kind": "archived_songbook", "url": null, "is_bardic_resource": false},
					{"id": 12, "name": "The Northmarch Songbook", "kind": "songbook_pdf",
						"url": "https://example.org/songbook.pdf", "is_bardic_resource": true}
				], "total": 2, "letters": [{"letter": "0", "count": 1}, {"letter": "N", "count": 1}]}`,
		},
		{
			name:       emptyListsCase,
			want:       defaultQuery,
			wantStatus: http.StatusOK,
			wantBody:   emptyLetterListBody,
		},
		{
			name:  everyParameterCase,
			query: "?kind=wiki&kind=songbook_pdf&letter=N&limit=100&offset=10000",
			want: datasources.CollectionQuery{
				Kinds:  []domain.CollectionKind{domain.CollectionKindWiki, domain.CollectionKindSongbookPDF},
				Letter: &letterN, Page: widestPage,
			},
			wantStatus: http.StatusOK,
			wantBody:   emptyLetterListBody,
		},
		{
			name:       listFailureCase,
			want:       defaultQuery,
			listErr:    errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lister := mocks.NewPublicCollectionLister(t)
			lister.EXPECT().ListPublicCollections(mock.Anything, tc.want).Return(tc.list, tc.listErr)

			rec := serveGet(t, CollectionListGet{Lister: lister}, collectionsPath+tc.query, nil, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	invalid := append([]invalidQueryCase{
		{name: unknownKindCase, query: "kind=library",
			wantBody: invalidKindBody},
		{name: duplicateKindsCase, query: "kind=wiki&kind=wiki",
			wantBody: duplicateKindBody},
		{name: "letter_lower_case", query: "letter=n", wantBody: invalidLetterBody},
		{name: "letter_repeated", query: "letter=N&letter=N",
			wantBody: `{"code":"invalid_request","message":"query parameter letter must be given at most once"}`},
	}, invalidPageCases...)
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := serveGet(t, CollectionListGet{Lister: mocks.NewPublicCollectionLister(t)},
				collectionsPath+"?"+tc.query, nil, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
