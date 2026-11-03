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

const needsWorkPath = "/v1/needs-work"

func TestNeedsWorkListGet_ServeHTTP(t *testing.T) {
	t.Parallel()

	defaultPage := domain.Page{Limit: domain.DefaultPageLimit}
	deadLinks := domain.NeedsWorkReasonDeadLinks
	counts := []domain.NeedsWorkCount{
		{Reason: domain.NeedsWorkReasonStub, Count: 1},
		{Reason: domain.NeedsWorkReasonUnassessed, Count: 0},
		{Reason: domain.NeedsWorkReasonMissingLicensing, Count: 1},
		{Reason: domain.NeedsWorkReasonDeadLinks, Count: 2},
	}
	countsBody := `[{"reason": "stub", "count": 1}, {"reason": "unassessed", "count": 0},
		{"reason": "missing_licensing", "count": 1}, {"reason": "dead_links", "count": 2}]`

	cases := []struct {
		name       string
		query      string
		wantReason *domain.NeedsWorkReason
		wantPage   domain.Page
		list       domain.NeedsWorkList
		listErr    error
		wantStatus int
		wantBody   string
	}{
		{
			name:     "every_reason",
			wantPage: defaultPage,
			list: domain.NeedsWorkList{
				Entries: []domain.NeedsWorkEntry{
					{EntityType: domain.EntityTypePiece, Slug: testPieceSlug, Title: testPieceTitle,
						Reasons: []domain.NeedsWorkReason{domain.NeedsWorkReasonStub, domain.NeedsWorkReasonMissingLicensing}},
					{EntityType: domain.EntityTypePerson, Slug: testPersonSlug, Title: testPersonName,
						Reasons: []domain.NeedsWorkReason{domain.NeedsWorkReasonDeadLinks}},
				},
				Total:  2,
				Counts: counts,
			},
			wantStatus: http.StatusOK,
			wantBody: `{"data": [
					{"entity_type": "piece", "slug": "the-copper-kettle", "title": "The Copper Kettle",
						"reasons": ["stub", "missing_licensing"]},
					{"entity_type": "person", "slug": "wren-of-the-vale", "title": "Wren of the Vale",
						"reasons": ["dead_links"]}
				], "total": 2, "counts": ` + countsBody + `}`,
		},
		{
			name:       everyParameterCase,
			query:      "reason=dead_links&limit=100&offset=10000",
			wantReason: &deadLinks,
			wantPage:   widestPage,
			list:       domain.NeedsWorkList{Total: 2, Counts: counts},
			wantStatus: http.StatusOK,
			wantBody:   `{"data": [], "total": 2, "counts": ` + countsBody + `}`,
		},
		{
			name:       emptyListsCase,
			wantPage:   defaultPage,
			wantStatus: http.StatusOK,
			wantBody:   `{"data": [], "total": 0, "counts": []}`,
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

			lister := mocks.NewPublicNeedsWorkLister(t)
			lister.EXPECT().ListPublicNeedsWork(mock.Anything, tc.wantReason, tc.wantPage).Return(tc.list, tc.listErr)

			rec := serveGet(t, NeedsWorkListGet{Lister: lister}, needsWorkPath+"?"+tc.query, nil, false)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}

	invalidReasonBody := `{"code":"invalid_request","message":"invalid reason: not one of its allowed values"}`
	invalid := append([]invalidQueryCase{
		{name: "unknown_reason", query: "reason=lyrics", wantBody: invalidReasonBody},
		{name: "empty_reason", query: "reason=", wantBody: invalidReasonBody},
		{name: "upper_case_reason", query: "reason=STUB", wantBody: invalidReasonBody},
		{
			name: "repeated_reason", query: "reason=stub&reason=unassessed",
			wantBody: `{"code":"invalid_request","message":"query parameter reason must be given at most once"}`,
		},
	}, invalidPageCases...)
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mock fails the test if the controller reads it.
			rec := serveGet(t, NeedsWorkListGet{Lister: mocks.NewPublicNeedsWorkLister(t)},
				needsWorkPath+"?"+tc.query, nil, true)

			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
