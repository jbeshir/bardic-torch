package controller

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
)

const principalUserID = "google-oauth2|123"

var testNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

func testClock() time.Time {
	return testNow
}

// asPrincipal returns req authenticated with Auth0 as a contributor with ID principalUserID, as the auth and
// require* middleware would leave it.
func asPrincipal(req *http.Request) *http.Request {
	principal := domain.Principal{
		User:   domain.User{ID: principalUserID, DisplayName: testDisplayName, Role: domain.RoleContributor, CreatedAt: testNow},
		Method: domain.AuthMethodAuth0,
	}
	return req.WithContext(domain.ContextWithPrincipal(req.Context(), principal))
}

const (
	testDisplayName = "Morwen"

	anonymousCase = "anonymous"
	tooLargeCase  = "too_large"
	notJSONCase   = "not_json"
	notFoundCase  = "not_found"

	testPieceTitle      = "The Copper Kettle"
	testPersonName      = "Wren of the Vale"
	testTermName        = "War"
	testEventName       = "Midwinter Revel"
	testVideoURL        = "https://video.example.org/watch/kettle"
	testAffiliationName = "Northmarch"
	testAffiliationSlug = "northmarch"

	slugVar         = "slug"
	collectionIDVar = "collection_id"

	everyParameterCase = "every_parameter"
	unknownKindCase    = "unknown_kind"
	duplicateKindsCase = "duplicate_kinds"
	getFailureCase     = "get_failure"
	listFailureCase    = "list_failure"
	emptyListsCase     = "empty_lists"

	unauthenticatedBody      = `{"code":"unauthenticated","message":"authentication required"}`
	internalErrorBody        = `{"code":"internal","message":"internal error"}`
	invalidKindBody          = `{"code":"invalid_request","message":"invalid kind: not one of its allowed values"}`
	duplicateKindBody        = `{"code":"invalid_request","message":"query parameter kind must not repeat a value"}`
	tooLargeBody             = `{"code":"request_too_large","message":"request body is too large"}`
	unsupportedMediaTypeBody = `{"code":"unsupported_media_type","message":"request body must be sent as application/json"}`
)

// TestWriteInternalError_DeadlineExceededIs503 requires an error of a request whose budget ran out (its context's
// deadline passed) to be 503 unavailable, and a deadline error of some other context, the request's still live, to
// stay 500 internal.
func TestWriteInternalError_DeadlineExceededIs503(t *testing.T) {
	t.Parallel()

	expired, cancel := context.WithDeadline(context.Background(), time.Unix(0, 0))
	t.Cleanup(cancel)
	tests := []struct {
		name       string
		ctx        context.Context
		err        error
		wantStatus int
		wantBody   string
	}{
		{name: "budget_ran_out", ctx: expired, err: fmt.Errorf("listing pieces: %w", context.DeadlineExceeded),
			wantStatus: http.StatusServiceUnavailable, wantBody: unavailableBody},
		{name: "deadline_of_another_context", ctx: context.Background(), err: context.DeadlineExceeded,
			wantStatus: http.StatusInternalServerError, wantBody: internalErrorBody},
		{name: "other_error_after_budget", ctx: expired, err: errCommand,
			wantStatus: http.StatusInternalServerError, wantBody: internalErrorBody},
		{name: "database_unavailable", ctx: context.Background(), err: datasources.ErrUnavailable,
			wantStatus: http.StatusServiceUnavailable, wantBody: unavailableBody},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequestWithContext(tc.ctx, http.MethodGet, "/v1/pieces", nil)
			w := httptest.NewRecorder()
			WriteInternalError(w, r, "unable to list pieces", tc.err)
			assert.Equal(t, tc.wantStatus, w.Code)
			assert.JSONEq(t, tc.wantBody, w.Body.String())
		})
	}
}
