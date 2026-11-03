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
	testEventSlug = "midwinter-revel"
	eventsPath    = "/v1/events"
)

func TestEventListGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	venue := domain.EventKindVenue
	defaultPage := domain.Page{Limit: domain.DefaultPageLimit}

	cases := []struct {
		name       string
		query      string
		wantKind   *domain.EventKind
		wantPage   domain.Page
		list       domain.EventList
		listErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:     "events",
			wantPage: defaultPage,
			list: domain.EventList{
				Events: []domain.EventSummary{
					{Slug: "the-green-hall", Name: "The Green Hall", Kind: domain.EventKindVenue},
					{Slug: testEventSlug, Name: testEventName, Kind: domain.EventKindEvent},
				},
				Total: 60,
			},
			wantStatus: http.StatusOK,
			wantBody: `{"data": [
				{"slug": "the-green-hall", "name": "The Green Hall", "kind": "venue"},
				{"slug": "midwinter-revel", "name": "Midwinter Revel", "kind": "event"}
			], "total": 60}`,
		},
		{
			name:       "kind_and_page",
			query:      "?kind=venue&limit=100&offset=10000",
			wantKind:   &venue,
			wantPage:   widestPage,
			list:       domain.EventList{Total: 3},
			wantStatus: http.StatusOK,
			wantBody:   `{"data": [], "total": 3}`,
		},
		{
			name:       listFailureCase,
			wantPage:   defaultPage,
			listErr:    errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			lister := mocks.NewPublicEventLister(t)
			lister.EXPECT().ListPublicEvents(mock.Anything, tc.wantKind, tc.wantPage).Return(tc.list, tc.listErr)

			rec := serveGet(t, EventListGet{Lister: lister}, eventsPath+tc.query, nil, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	invalid := append([]invalidQueryCase{
		{name: unknownKindCase, query: "kind=festival", wantBody: invalidKindBody},
		{name: "upper_case_kind", query: "kind=Venue", wantBody: invalidKindBody},
		{
			name: "repeated_kind", query: "kind=venue&kind=event",
			wantBody: `{"code":"invalid_request","message":"query parameter kind must be given at most once"}`,
		},
	}, invalidPageCases...)
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveGet(t, EventListGet{Lister: mocks.NewPublicEventLister(t)}, eventsPath+"?"+tc.query, nil, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
