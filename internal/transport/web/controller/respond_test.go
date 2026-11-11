package controller

import (
	"net/http"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

const (
	principalUserID = "google-oauth2|123"
	testDisplayName = "Morwen"

	internalErrorBody = `{"code":"internal","message":"internal error"}`
)

var testNow = time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)

// asPrincipal returns req authenticated with Auth0 as a contributor with ID principalUserID, as the auth and
// require* middleware would leave it.
func asPrincipal(req *http.Request) *http.Request {
	principal := domain.Principal{
		User: domain.User{ID: principalUserID, DisplayName: testDisplayName, Role: domain.RoleContributor, CreatedAt: testNow},
	}
	return req.WithContext(domain.ContextWithPrincipal(req.Context(), principal))
}
