package controller

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const (
	testTermSlug = "war"
	termsPath    = "/v1/terms"
)

func TestTermListGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	affiliation := domain.FacetAffiliation
	terms := []domain.TermListItem{
		{
			TermSummary: domain.TermSummary{Slug: testTermSlug, Facet: domain.FacetTopic, DisplayName: testTermName},
			Name:        testTermName,
			TermCounts:  domain.TermCounts{PieceCount: 3},
		},
		{
			TermSummary: domain.TermSummary{Slug: "swords", Facet: domain.FacetTopic, DisplayName: "Swords"},
			Name:        "Swords (Topic)",
			ParentSlug:  new(testTermSlug),
			TermCounts:  domain.TermCounts{PieceCount: 1, PersonCount: 2},
		},
	}

	cases := []struct {
		name       string
		query      string
		wantFacet  *domain.Facet
		terms      []domain.TermListItem
		listErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "every_facet",
			terms:      terms,
			wantStatus: http.StatusOK,
			wantBody: `{"data": [
				{"slug": "war", "facet": "topic", "name": "War", "display_name": "War", "parent_slug": null,
					"piece_count": 3, "person_count": 0},
				{"slug": "swords", "facet": "topic", "name": "Swords (Topic)", "display_name": "Swords",
					"parent_slug": "war", "piece_count": 1, "person_count": 2}
			]}`,
		},
		{
			name:       "one_facet",
			query:      "?facet=affiliation",
			wantFacet:  &affiliation,
			wantStatus: http.StatusOK,
			wantBody:   `{"data": []}`,
		},
		{
			name:       listFailureCase,
			listErr:    errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lister := mocks.NewPublicTermLister(t)
			lister.EXPECT().ListPublicTerms(mock.Anything, tc.wantFacet).Return(tc.terms, tc.listErr)

			rec := serveGet(t, TermListGet{Lister: lister}, termsPath+tc.query, nil, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	invalidFacetBody := `{"code":"invalid_request","message":"invalid facet: not one of its allowed values"}`
	for _, tc := range []invalidQueryCase{
		{name: "kind_is_not_a_facet", query: "facet=kind", wantBody: invalidFacetBody},
		{name: "upper_case", query: "facet=Topic", wantBody: invalidFacetBody},
		{name: "empty", query: "facet=", wantBody: invalidFacetBody},
		{
			name: "repeated", query: "facet=topic&facet=mood",
			wantBody: `{"code":"invalid_request","message":"query parameter facet must be given at most once"}`,
		},
		{name: "bad_encoding", query: "facet=%zz", wantBody: invalidQueryEncodingBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveGet(t, TermListGet{Lister: mocks.NewPublicTermLister(t)}, termsPath+"?"+tc.query, nil, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
