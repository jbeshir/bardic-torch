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

// testTerm has every field and list of a term set, including the related affiliation.
func testTerm() domain.Term {
	return domain.Term{
		TermSummary:   domain.TermSummary{Slug: testTermSlug, Facet: domain.FacetTopic, DisplayName: testTermName},
		Name:          "War (Topic)",
		UpdatedAt:     testRevisionCreatedAt,
		DescriptionMD: new("Songs of war."),
		Aliases:       []string{"Battle", "Strife"},
		Ancestors: []domain.TermSummary{
			{Slug: "conflict", Facet: domain.FacetTopic, DisplayName: "Conflict"},
		},
		Children: []domain.TermChild{{
			Term:       domain.TermSummary{Slug: "swords", Facet: domain.FacetTopic, DisplayName: "Swords"},
			TermCounts: domain.TermCounts{PieceCount: 1},
		}},
		RelatedAffiliation: &domain.TermSummary{
			Slug: testAffiliationSlug, Facet: domain.FacetAffiliation, DisplayName: testAffiliationName,
		},
		TermCounts: domain.TermCounts{PieceCount: 7, PersonCount: 1},
		Pieces: []domain.PieceSummary{{
			Slug: testPieceSlug, Title: testPieceTitle, Kind: domain.PieceKindSong,
			SecondaryKinds: []domain.PieceKind{domain.PieceKindPoem}, ConservationStatus: domain.ConservationStatusEndangered,
		}},
	}
}

func TestTermGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	affiliation := domain.Term{
		TermSummary: domain.TermSummary{Slug: testTermSlug, Facet: domain.FacetAffiliation, DisplayName: testTermName},
		Name:        testTermName,
		UpdatedAt:   testRevisionCreatedAt,
		TermCounts:  domain.TermCounts{PersonCount: 1},
		Persons: []domain.PersonSummary{
			{Slug: testPersonSlug, Name: testPersonName, Kind: domain.PersonKindCommunityBard, IsStub: true},
		},
	}

	cases := []struct {
		name       string
		query      string
		wantPage   domain.Page
		term       domain.Term
		getErr     error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "term",
			wantPage:   domain.Page{Limit: domain.DefaultPageLimit},
			term:       testTerm(),
			wantStatus: http.StatusOK,
			wantBody: `{
				"slug": "war", "facet": "topic", "name": "War (Topic)", "display_name": "War",
				"updated_at": "2026-10-01T09:00:00Z",
				"description_md": "Songs of war.", "aliases": ["Battle", "Strife"],
				"ancestors": [{"slug": "conflict", "facet": "topic", "display_name": "Conflict"}],
				"children": [{"term": {"slug": "swords", "facet": "topic", "display_name": "Swords"},
					"piece_count": 1, "person_count": 0}],
				"related_affiliation": {"slug": "northmarch", "facet": "affiliation", "display_name": "Northmarch"},
				"piece_count": 7, "person_count": 1,
				"pieces": [{"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "song",
					"secondary_kinds": ["poem"], "conservation_status": "endangered", "is_stub": false}],
				"persons": []
			}`,
		},
		{
			name:       "affiliation_with_empty_lists",
			query:      "?limit=100&offset=10000",
			wantPage:   widestPage,
			term:       affiliation,
			wantStatus: http.StatusOK,
			wantBody: `{
				"slug": "war", "facet": "affiliation", "name": "War", "display_name": "War",
				"updated_at": "2026-10-01T09:00:00Z",
				"description_md": null, "aliases": [], "ancestors": [], "children": [], "related_affiliation": null,
				"piece_count": 0, "person_count": 1, "pieces": [],
				"persons": [{"slug": "wren-of-the-vale", "name": "Wren of the Vale", "kind": "community_bard",
					"is_stub": true}]
			}`,
		},
		{
			name:       notFoundCase,
			query:      "?limit=1&offset=0",
			wantPage:   domain.Page{Limit: 1},
			getErr:     datasources.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"code":"not_found","message":"term not found"}`,
		},
		{
			name:       getFailureCase,
			wantPage:   domain.Page{Limit: domain.DefaultPageLimit},
			getErr:     errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			getter := mocks.NewPublicTermGetter(t)
			getter.EXPECT().GetPublicTerm(mock.Anything, testTermSlug, tc.wantPage).Return(tc.term, tc.getErr)

			rec := serveGet(t, TermGet{Getter: getter}, termsPath+"/"+testTermSlug+tc.query,
				map[string]string{slugVar: testTermSlug}, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidSlugCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveSlug(t, TermGet{Getter: mocks.NewPublicTermGetter(t)}, "terms", tc.pathSlug, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidPageCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := serveGet(t, TermGet{Getter: mocks.NewPublicTermGetter(t)},
				termsPath+"/"+testTermSlug+"?"+tc.query, map[string]string{slugVar: testTermSlug}, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
