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

func TestEventGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	event := domain.Event{
		EventSummary:  domain.EventSummary{Slug: testEventSlug, Name: testEventName, Kind: domain.EventKindEvent},
		UpdatedAt:     testRevisionCreatedAt,
		DescriptionMD: new("A winter gathering."),
		Performances: []domain.EventPerformance{
			{
				Piece: &domain.PieceSummary{
					Slug: testPieceSlug, Title: testPieceTitle, Kind: domain.PieceKindSong,
					ConservationStatus: domain.ConservationStatusUnassessed, IsStub: true,
				},
				Link: domain.PerformanceLink{
					URL: testVideoURL, Title: new("At the revel"),
					Platform: new("video.example.org"),
					Liveness: domain.LivenessOK, LastCheckedAt: &testRevisionCreatedAt,
					ArchivedURL: new("https://archive.example.org/kettle"),
				},
				Year: new(int16(2025)),
				Performers: []domain.PersonSummary{
					{Slug: testPersonSlug, Name: testPersonName, Kind: domain.PersonKindCommunityBard},
				},
				DescriptionVerbatim: "Wren sings the Kettle",
			},
			{
				Link:                domain.PerformanceLink{URL: "https://example.org/opening", Liveness: domain.LivenessUnknown},
				DescriptionVerbatim: "Opening",
			},
		},
		PerformancesTotal: 201,
	}
	empty := domain.Event{
		EventSummary: domain.EventSummary{Slug: testEventSlug, Name: testEventName, Kind: domain.EventKindVenue},
		UpdatedAt:    testRevisionCreatedAt,
	}

	cases := []struct {
		name       string
		event      domain.Event
		getErr     error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "event",
			event:      event,
			wantStatus: http.StatusOK,
			wantBody: `{
				"slug": "midwinter-revel", "name": "Midwinter Revel", "kind": "event",
				"updated_at": "2026-10-01T09:00:00Z", "description_md": "A winter gathering.",
				"performances": [
					{"piece": {"slug": "the-copper-kettle", "title": "The Copper Kettle", "kind": "song",
						"secondary_kinds": [], "conservation_status": "unassessed", "is_stub": true},
						"link": {"url": "https://video.example.org/watch/kettle", "title": "At the revel",
							"platform": "video.example.org", "liveness": "ok",
							"last_checked_at": "2026-10-01T09:00:00Z", "archived_url": "https://archive.example.org/kettle"},
						"year": 2025,
						"performers": [{"slug": "wren-of-the-vale", "name": "Wren of the Vale",
							"kind": "community_bard", "is_stub": false}],
						"description_verbatim": "Wren sings the Kettle"},
					{"piece": null, "link": {"url": "https://example.org/opening", "title": null, "platform": null,
						"liveness": "unknown", "last_checked_at": null, "archived_url": null},
						"year": null, "performers": [], "description_verbatim": "Opening"}
				],
				"performances_total": 201
			}`,
		},
		{
			name:       emptyListsCase,
			event:      empty,
			wantStatus: http.StatusOK,
			wantBody: `{"slug": "midwinter-revel", "name": "Midwinter Revel", "kind": "venue",
				"updated_at": "2026-10-01T09:00:00Z", "description_md": null, "performances": [], "performances_total": 0}`,
		},
		{
			name:       notFoundCase,
			getErr:     datasources.ErrNotFound,
			wantStatus: http.StatusNotFound,
			wantBody:   `{"code":"not_found","message":"event not found"}`,
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

			getter := mocks.NewPublicEventGetter(t)
			getter.EXPECT().GetPublicEvent(mock.Anything, testEventSlug).Return(tc.event, tc.getErr)

			rec := serveSlug(t, EventGet{Getter: getter}, "events", testEventSlug, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	for _, tc := range invalidSlugCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveSlug(t, EventGet{Getter: mocks.NewPublicEventGetter(t)}, "events", tc.pathSlug, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
