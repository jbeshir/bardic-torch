package controller

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// historyCase is a request for an entity's history, the history or error the datasource gives for the page the
// request asks for, and the response.
type historyCase struct {
	name       string
	query      string
	wantPage   domain.Page
	history    domain.RevisionHistory
	listErr    error
	wantStatus int
	wantBody   string
}

// historyCases are the history requests of every history controller; notFound is its 404 message.
func historyCases(notFound string) []historyCase {
	defaultPage := domain.Page{Limit: domain.DefaultPageLimit}
	return []historyCase{
		{
			name:     "history",
			wantPage: defaultPage,
			history: domain.RevisionHistory{
				Revisions: []domain.RevisionSummary{
					{
						ID: 43, Source: domain.RevisionSourceUser, Summary: "Credit the tune.",
						Author: &domain.RevisionAuthor{DisplayName: testDisplayName}, CreatedAt: testRevisionCreatedAt,
					},
					{
						ID: 42, Source: domain.RevisionSourceImport,
						Contributors: []string{"Archivist", "Lorekeeper"}, CreatedAt: testNow,
					},
				},
				Total: 3,
			},
			wantStatus: http.StatusOK,
			wantBody: `{"data": [
					{"id": 43, "source": "user", "summary": "Credit the tune.", "author": {"display_name": "Morwen"},
						"contributors": [], "created_at": "2026-10-01T09:00:00Z"},
					{"id": 42, "source": "import", "summary": "", "author": null,
						"contributors": ["Archivist", "Lorekeeper"], "created_at": "2026-10-31T12:00:00Z"}
				], "total": 3}`,
		},
		{
			name:       emptyListsCase,
			query:      "limit=100&offset=10000",
			wantPage:   widestPage,
			wantStatus: http.StatusOK,
			wantBody:   `{"data": [], "total": 0}`,
		},
		{
			name:       notFoundCase,
			wantPage:   defaultPage,
			listErr:    datasources.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"code":"not_found","message":"` + notFound + `"}`,
		},
		{
			name:       listFailureCase,
			wantPage:   defaultPage,
			listErr:    errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
	}
}

func TestHistoryGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	// The history path of each entity type, given a slug.
	for pathOf, entityType := range map[string]domain.SlugEntityType{
		"/v1/pieces/%s/history": domain.SlugEntityTypePiece, "/v1/persons/%s/history": domain.SlugEntityTypePerson,
		"/v1/terms/%s/history": domain.SlugEntityTypeTerm, "/v1/events/%s/history": domain.SlugEntityTypeEvent,
	} {
		t.Run(entityType.String(), func(t *testing.T) {
			t.Parallel()

			target := fmt.Sprintf(pathOf, testPieceSlug)
			for _, tc := range historyCases(entityType.String() + " not found") {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					lister := mocks.NewPublicHistoryLister(t)
					lister.EXPECT().ListPublicHistory(mock.Anything, entityType, testPieceSlug, tc.wantPage).
						Return(tc.history, tc.listErr)

					rec := serveGet(t, HistoryGet{Lister: lister, EntityType: entityType},
						target+"?"+tc.query, map[string]string{slugVar: testPieceSlug}, false)

					assert.Equal(t, tc.wantStatus, rec.Code)
					assert.JSONEq(t, tc.wantBody, rec.Body.String())
				})
			}

			for _, tc := range invalidSlugCases {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					// The mock fails the test if the controller reads it.
					rec := serveGet(t, HistoryGet{Lister: mocks.NewPublicHistoryLister(t), EntityType: entityType},
						fmt.Sprintf(pathOf, url.PathEscape(tc.pathSlug)),
						map[string]string{slugVar: tc.pathSlug}, true)

					assert.Equal(t, http.StatusBadRequest, rec.Code)
					assert.JSONEq(t, tc.wantBody, rec.Body.String())
				})
			}

			for _, tc := range invalidPageCases {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()

					rec := serveGet(t, HistoryGet{Lister: mocks.NewPublicHistoryLister(t), EntityType: entityType},
						target+"?"+tc.query, map[string]string{slugVar: testPieceSlug}, true)

					assert.Equal(t, http.StatusBadRequest, rec.Code)
					assert.JSONEq(t, tc.wantBody, rec.Body.String())
				})
			}
		})
	}
}

func TestCollectionHistoryGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	const target = "/v1/collections/12/history"
	vars := map[string]string{collectionIDVar: "12"}

	for _, tc := range historyCases("collection not found") {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lister := mocks.NewPublicCollectionHistoryLister(t)
			lister.EXPECT().ListPublicCollectionHistory(mock.Anything, int64(12), tc.wantPage).
				Return(tc.history, tc.listErr)

			rec := serveGet(t, CollectionHistoryGet{Lister: lister}, target+"?"+tc.query, vars, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidIDCases(collectionIDVar) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveGet(t, CollectionHistoryGet{Lister: mocks.NewPublicCollectionHistoryLister(t)},
				"/v1/collections/1/history", map[string]string{collectionIDVar: tc.pathID}, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidPageCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := serveGet(t, CollectionHistoryGet{Lister: mocks.NewPublicCollectionHistoryLister(t)},
				target+"?"+tc.query, vars, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
