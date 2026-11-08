package controller

import (
	"errors"
	"net/http"
	"net/url"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// listCase is the paged-list case whose datasource answers with a list.
const listCase = "list"

// pagedListCase is a request for a paged list, the page the controller must ask for, what the datasource answers
// with (a body for success, or an error), and the response.
type pagedListCase struct {
	name       string
	query      string
	wantPage   domain.Page
	listErr    error
	wantStatus int
	wantBody   string
}

// pagedListCases are the requests every paged-list controller answers: listJSON is the body for the list the
// datasource gives for the "list" case, and notFound the 404 message.
func pagedListCases(listJSON, notFound string) []pagedListCase {
	defaultPage := domain.Page{Limit: domain.DefaultPageLimit}
	return []pagedListCase{
		{name: listCase, query: "limit=2&offset=1", wantPage: domain.Page{Limit: 2, Offset: 1},
			wantStatus: http.StatusOK, wantBody: listJSON},
		{name: emptyListsCase, query: "limit=100&offset=10000", wantPage: widestPage, wantStatus: http.StatusOK,
			wantBody: `{"data": [], "total": 0}`},
		{name: notFoundCase, wantPage: defaultPage, listErr: datasources.ErrNotFound, wantStatus: http.StatusNotFound,
			wantBody: `{"code":"not_found","message":"` + notFound + `"}`},
		{name: listFailureCase, wantPage: defaultPage, listErr: errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError, wantBody: internalErrorBody},
	}
}

var (
	pagedPerformanceLink = domain.PerformanceLink{
		URL: testVideoURL, Title: new("At the revel"), Platform: new("video.example.org"),
		Liveness: domain.LivenessOK, LastCheckedAt: &testRevisionCreatedAt,
	}
	pagedPerformanceLinkJSON = `{"url": "` + testVideoURL + `", "title": "At the revel", ` +
		`"platform": "video.example.org", "liveness": "ok", "last_checked_at": "2026-10-01T09:00:00Z", ` +
		`"archived_url": null}`
	pagedPiece = domain.PieceSummary{
		Slug: testPieceSlug, Title: testPieceTitle, Kind: domain.PieceKindSong,
		ConservationStatus: domain.ConservationStatusLowRisk, CreditNames: []string{testPersonName},
	}
	pagedPieceJSON = `{"slug": "` + testPieceSlug + `", "title": "` + testPieceTitle + `", "kind": "song", ` +
		`"secondary_kinds": [], "conservation_status": "low_risk", "is_stub": false, ` +
		`"credit_names": ["` + testPersonName + `"]}`
)

func TestPersonPerformancesGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	const target = "/v1/persons/" + testPersonSlug + "/performances"
	vars := map[string]string{slugVar: testPersonSlug}
	list := domain.PersonPerformanceList{
		Performances: []domain.PersonPerformance{
			{Piece: &pagedPiece, Link: pagedPerformanceLink, Year: new(int16(2025)), DescriptionVerbatim: "Wren sings"},
			{Link: pagedPerformanceLink, DescriptionVerbatim: "Wren opens the evening",
				Event: &domain.EventSummary{Slug: "the-oak-hall", Name: "The Oak Hall", Kind: domain.EventKindVenue}},
		},
		Total: 4,
	}
	listJSON := `{"data": [
			{"piece": ` + pagedPieceJSON + `, "link": ` + pagedPerformanceLinkJSON + `, "year": 2025, "event": null,
				"description_verbatim": "Wren sings"},
			{"piece": null, "link": ` + pagedPerformanceLinkJSON + `, "year": null,
				"event": {"slug": "the-oak-hall", "name": "The Oak Hall", "kind": "venue"},
				"description_verbatim": "Wren opens the evening"}
		], "total": 4}`

	for _, tc := range pagedListCases(listJSON, "person not found") {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var answer domain.PersonPerformanceList
			if tc.name == listCase {
				answer = list
			}
			lister := mocks.NewPublicPersonPerformanceLister(t)
			lister.EXPECT().ListPublicPersonPerformances(mock.Anything, testPersonSlug, tc.wantPage).
				Return(answer, tc.listErr)

			rec := serveGet(t, PersonPerformancesGet{Lister: lister}, target+"?"+tc.query, vars, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidSlugCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveGet(t, PersonPerformancesGet{Lister: mocks.NewPublicPersonPerformanceLister(t)},
				"/v1/persons/"+url.PathEscape(tc.pathSlug)+"/performances", map[string]string{slugVar: tc.pathSlug}, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidPageCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := serveGet(t, PersonPerformancesGet{Lister: mocks.NewPublicPersonPerformanceLister(t)},
				target+"?"+tc.query, vars, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}

func TestCollectionCitationsGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	const target = "/v1/collections/12/citations"
	vars := map[string]string{collectionIDVar: "12"}
	list := domain.CitationList{
		Citations: []domain.Citation{
			{Piece: &pagedPiece, URL: "https://example.org/songbook.pdf", Title: new(testCollectionName),
				Locator: new("p. 12"), Liveness: domain.LivenessOK, Roles: []domain.LinkRole{domain.LinkRoleLyrics}},
			{Person: &domain.PersonSummary{Slug: testPersonSlug, Name: testPersonName, Kind: domain.PersonKindCommunityBard},
				URL: "https://example.org/songbook.pdf#wren", Liveness: domain.LivenessUnknown,
				Roles: []domain.LinkRole{domain.LinkRoleOther}},
		},
		Total: 3,
	}
	listJSON := `{"data": [
			{"piece": ` + pagedPieceJSON + `, "person": null, "url": "https://example.org/songbook.pdf",
				"title": "` + testCollectionName + `", "locator": "p. 12", "liveness": "ok", "last_checked_at": null,
				"archived_url": null, "roles": ["lyrics"]},
			{"piece": null, "person": {"slug": "` + testPersonSlug + `", "name": "` + testPersonName + `",
				"kind": "community_bard", "is_stub": false}, "url": "https://example.org/songbook.pdf#wren",
				"title": null, "locator": null, "liveness": "unknown", "last_checked_at": null, "archived_url": null,
				"roles": ["other"]}
		], "total": 3}`

	for _, tc := range pagedListCases(listJSON, "collection not found") {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var answer domain.CitationList
			if tc.name == listCase {
				answer = list
			}
			lister := mocks.NewPublicCollectionCitationLister(t)
			lister.EXPECT().ListPublicCollectionCitations(mock.Anything, int64(12), tc.wantPage).
				Return(answer, tc.listErr)

			rec := serveGet(t, CollectionCitationsGet{Lister: lister}, target+"?"+tc.query, vars, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidIDCases(collectionIDVar) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := serveGet(t, CollectionCitationsGet{Lister: mocks.NewPublicCollectionCitationLister(t)},
				"/v1/collections/1/citations", map[string]string{collectionIDVar: tc.pathID}, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidPageCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := serveGet(t, CollectionCitationsGet{Lister: mocks.NewPublicCollectionCitationLister(t)},
				target+"?"+tc.query, vars, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}

func TestEventPerformancesGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	const target = "/v1/events/" + testPieceSlug + "/performances"
	vars := map[string]string{slugVar: testPieceSlug}
	list := domain.EventPerformanceList{
		Performances: []domain.EventPerformance{
			{Piece: &pagedPiece, Link: pagedPerformanceLink, Year: new(int16(2025)), DescriptionVerbatim: "Wren sings",
				Performers: []domain.PersonSummary{
					{Slug: testPersonSlug, Name: testPersonName, Kind: domain.PersonKindCommunityBard},
				}},
			{Link: pagedPerformanceLink, DescriptionVerbatim: "The hall sings"},
		},
		Total: 2,
	}
	listJSON := `{"data": [
			{"piece": ` + pagedPieceJSON + `, "link": ` + pagedPerformanceLinkJSON + `, "year": 2025,
				"performers": [{"slug": "` + testPersonSlug + `", "name": "` + testPersonName + `",
					"kind": "community_bard", "is_stub": false}],
				"description_verbatim": "Wren sings"},
			{"piece": null, "link": ` + pagedPerformanceLinkJSON + `, "year": null, "performers": [],
				"description_verbatim": "The hall sings"}
		], "total": 2}`

	for _, tc := range pagedListCases(listJSON, "event not found") {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var answer domain.EventPerformanceList
			if tc.name == listCase {
				answer = list
			}
			lister := mocks.NewPublicEventPerformanceLister(t)
			lister.EXPECT().ListPublicEventPerformances(mock.Anything, testPieceSlug, tc.wantPage).
				Return(answer, tc.listErr)

			rec := serveGet(t, EventPerformancesGet{Lister: lister}, target+"?"+tc.query, vars, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidSlugCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := serveGet(t, EventPerformancesGet{Lister: mocks.NewPublicEventPerformanceLister(t)},
				"/v1/events/"+url.PathEscape(tc.pathSlug)+"/performances", map[string]string{slugVar: tc.pathSlug}, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidPageCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := serveGet(t, EventPerformancesGet{Lister: mocks.NewPublicEventPerformanceLister(t)},
				target+"?"+tc.query, vars, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
