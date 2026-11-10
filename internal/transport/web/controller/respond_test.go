package controller

import (
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

const principalUserID = "google-oauth2|123"

var testNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// asPrincipal returns req authenticated with Auth0 as a contributor with ID principalUserID, as the auth and
// require* middleware would leave it.
func asPrincipal(req *http.Request) *http.Request {
	principal := domain.Principal{
		User: domain.User{ID: principalUserID, DisplayName: testDisplayName, Role: domain.RoleContributor, CreatedAt: testNow},
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
