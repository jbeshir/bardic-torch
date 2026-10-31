package controller

import (
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
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

	unauthenticatedBody      = `{"code":"unauthenticated","message":"authentication required"}`
	internalErrorBody        = `{"code":"internal","message":"internal error"}`
	tooLargeBody             = `{"code":"request_too_large","message":"request body is too large"}`
	unsupportedMediaTypeBody = `{"code":"unsupported_media_type","message":"request body must be sent as application/json"}`
)
