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

const testCollectionName = "The Northmarch Songbook"

// testCollection has every field and list of a collection set, with a piece citation and a person citation.
func testCollection() domain.Collection {
	return domain.Collection{
		CollectionSummary: domain.CollectionSummary{
			ID: 12, Name: testCollectionName, Kind: domain.CollectionKindSongbookPDF,
			URL: new("https://example.org/songbook.pdf"), IsBardicResource: true,
		},
		DescriptionMD: new("Songs of the north."),
		Enumerable:    domain.CollectionEnumerablePDF,
		Region:        &domain.TermSummary{Slug: testAffiliationSlug, Facet: domain.FacetAffiliation, DisplayName: testAffiliationName},
		Parent:        &domain.CollectionSummary{ID: 3, Name: "Songbooks of the Marches", Kind: domain.CollectionKindWiki},
		Children: []domain.CollectionSummary{
			{ID: 13, Name: "Northmarch Songbook Supplement", Kind: domain.CollectionKindHostedDocument},
		},
		Citations: []domain.Citation{
			{
				Piece: &domain.PieceSummary{
					Slug: testPieceSlug, Title: testPieceTitle, Kind: domain.PieceKindSong,
					ConservationStatus: domain.ConservationStatusLowRisk,
				},
				URL: "https://example.org/songbook.pdf", Title: new(testCollectionName), Locator: new("p. 12"),
				Roles: []domain.LinkRole{domain.LinkRoleLyrics, domain.LinkRoleSheetMusic},
			},
			{
				Person: &domain.PersonSummary{Slug: testPersonSlug, Name: testPersonName, Kind: domain.PersonKindCommunityBard},
				URL:    "https://example.org/songbook.pdf#wren",
				Roles:  []domain.LinkRole{domain.LinkRoleExternalProfile},
			},
		},
		CitationsTotal: 250,
	}
}

func TestCollectionGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	empty := domain.Collection{
		CollectionSummary: domain.CollectionSummary{ID: 12, Name: testCollectionName, Kind: domain.CollectionKindOther},
		Enumerable:        domain.CollectionEnumerableUnknown,
	}

	cases := []struct {
		name       string
		collection domain.Collection
		getErr     error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "collection",
			collection: testCollection(),
			wantStatus: http.StatusOK,
			wantBody: `{
				"id": 12, "name": "The Northmarch Songbook", "kind": "songbook_pdf",
				"url": "https://example.org/songbook.pdf", "is_bardic_resource": true,
				"description_md": "Songs of the north.", "enumerable": "pdf",
				"region": {"slug": "northmarch", "facet": "affiliation", "display_name": "Northmarch"},
				"parent": {"id": 3, "name": "Songbooks of the Marches", "kind": "wiki", "url": null,
					"is_bardic_resource": false},
				"children": [{"id": 13, "name": "Northmarch Songbook Supplement", "kind": "hosted_document",
					"url": null, "is_bardic_resource": false}],
				"citations": [
					{"piece": {"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "song",
						"secondary_kinds": [], "conservation_status": "low_risk", "is_stub": false},
						"person": null, "url": "https://example.org/songbook.pdf",
						"title": "The Northmarch Songbook", "locator": "p. 12", "roles": ["lyrics", "sheet_music"]},
					{"piece": null, "person": {"slug": "wren-of-the-vale", "name": "Wren of the Vale",
						"kind": "community_bard", "is_stub": false},
						"url": "https://example.org/songbook.pdf#wren", "title": null, "locator": null,
						"roles": ["external_profile"]}
				],
				"citations_total": 250
			}`,
		},
		{
			name:       emptyListsCase,
			collection: empty,
			wantStatus: http.StatusOK,
			wantBody: `{
				"id": 12, "name": "The Northmarch Songbook", "kind": "other", "url": null,
				"is_bardic_resource": false, "description_md": null, "enumerable": "unknown", "region": null,
				"parent": null, "children": [], "citations": [], "citations_total": 0
			}`,
		},
		{
			name:       notFoundCase,
			getErr:     datasources.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"code":"not_found","message":"collection not found"}`,
		},
		{
			name:       getFailureCase,
			getErr:     errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			getter := mocks.NewPublicCollectionGetter(t)
			getter.EXPECT().GetPublicCollection(mock.Anything, int64(12)).Return(tc.collection, tc.getErr)

			rec := serveGet(t, CollectionGet{Getter: getter}, "/v1/collections/12",
				map[string]string{collectionIDVar: "12"}, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	t.Run("largest_id", func(t *testing.T) {
		t.Parallel()

		getter := mocks.NewPublicCollectionGetter(t)
		getter.EXPECT().GetPublicCollection(mock.Anything, int64(9223372036854775807)).
			Return(domain.Collection{}, datasources.ErrNotFound)

		rec := serveGet(t, CollectionGet{Getter: getter}, "/v1/collections/9223372036854775807",
			map[string]string{collectionIDVar: "9223372036854775807"}, false)

		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	for _, tc := range invalidIDCases(collectionIDVar) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveGet(t, CollectionGet{Getter: mocks.NewPublicCollectionGetter(t)}, "/v1/collections/1",
				map[string]string{collectionIDVar: tc.pathID}, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
