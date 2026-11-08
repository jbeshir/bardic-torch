//go:build integration

package app

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/auth0test"
	"github.com/jbeshir/bardic-torch/internal/testutil/mysqltest"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/jbeshir/bardic-torch/internal/transport/web/router"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	endToEndAudience      = "https://api.bardictorch.org"
	endToEndContributorID = "google-oauth2|e2e-contributor"
	endToEndModeratorID   = "facebook|e2e-moderator"
	// endToEndContributorPath is the contributor's blocked state, with the "|" in their ID percent-encoded.
	endToEndContributorPath = "/v1/users/google-oauth2%7Ce2e-contributor/blocked"
)

// endToEnd sends requests to the production handler, checking each request and response against the spec. If
// logger is not nil, the handler logs each request to it.
type endToEnd struct {
	handler  http.Handler
	contract *openapitest.Validator
	logger   *slog.Logger
}

// send serves a request with the given Authorization header value and JSON body, either of which may be empty.
func (e endToEnd) send(t *testing.T, method, path, authorization, body string) *httptest.ResponseRecorder {
	t.Helper()

	ctx := t.Context()
	if e.logger != nil {
		ctx = domain.ContextWithLogger(ctx, e.logger)
	}
	req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return e.contract.Serve(t, e.handler, req)
}

// sendOK is send, requiring wantStatus and decoding the response body into a T.
func sendOK[T any](t *testing.T, e endToEnd, method, path, authorization, body string, wantStatus int) T {
	t.Helper()

	rec := e.send(t, method, path, authorization, body)
	require.Equal(t, wantStatus, rec.Code, rec.Body.String())

	var res T
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &res))
	return res
}

// sendRejected is send, requiring an error response with wantStatus and wantCode.
func sendRejected(
	t *testing.T, e endToEnd, method, path, authorization, body string, wantStatus int, wantCode httpjson.ErrorCode,
) {
	t.Helper()

	res := sendOK[errorResponse](t, e, method, path, authorization, body, wantStatus)
	assert.Equal(t, wantCode, res.Code)
}

// TestHTTP_EndToEnd runs the production handler against a real database, authenticating with the real Auth0
// validator (against a local issuer) and the real API token validator.
func TestHTTP_EndToEnd(t *testing.T) {
	t.Parallel()

	repository := mysql.New(mysqltest.NewDatabase(t))
	bootstrap := &command.BootstrapModerators{Promoter: repository, Now: now}
	_, err := bootstrap.Execute(t.Context(), []string{endToEndModeratorID})
	require.NoError(t, err)

	issuer := auth0test.NewIssuer(t)
	auth0Validator, err := router.NewAuth0Validator(issuer.URL, endToEndAudience)
	require.NoError(t, err)
	authValidators := []router.AuthValidator{
		auth0Validator,
		router.NewAPITokenValidator(repository, repository, now),
	}
	e := endToEnd{handler: NewHTTPHandler(repositoryHTTPDependencies(repository, authValidators, passingTurnstile{}, unlimitedRateLimits())), contract: openapitest.New(t)}

	auth0 := func(userID string) string {
		return "Bearer auth0|" + auth0test.Sign(t, issuer.Key, issuer.Claims(userID, endToEndAudience))
	}
	contributor, moderator := auth0(endToEndContributorID), auth0(endToEndModeratorID)

	sendOK[controller.HealthStatus](t, e, http.MethodGet, "/readyz", "", "", http.StatusOK)

	// Invalid Auth0 tokens for the contributor get the spec's 401, and do not create them.
	expiredClaims := issuer.Claims(endToEndContributorID, endToEndAudience)
	expiredClaims["exp"] = time.Now().Add(-time.Hour).Unix()
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for name, authorization := range map[string]string{
		"expired": "Bearer auth0|" + auth0test.Sign(t, issuer.Key, expiredClaims),
		"wrong_audience": "Bearer auth0|" +
			auth0test.Sign(t, issuer.Key, issuer.Claims(endToEndContributorID, "https://other.example")),
		"bad_signature": "Bearer auth0|" +
			auth0test.Sign(t, otherKey, issuer.Claims(endToEndContributorID, endToEndAudience)),
		// Over the validator's 8 KiB token cap.
		"oversized": "Bearer auth0|" + strings.Repeat("a", 9<<10),
	} {
		rec := e.send(t, http.MethodGet, "/v1/me", authorization, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, name)
		assert.JSONEq(t, `{"code":"unauthenticated","message":"invalid credentials"}`, rec.Body.String(), name)

		// Public catalogue reads skip authentication, so the real validator never sees these tokens there.
		rec = e.send(t, http.MethodGet, "/v1/pieces", authorization, "")
		assert.Equal(t, http.StatusOK, rec.Code, name)
	}

	// The first authenticated request creates the user, with a fresh public ID.
	first := sendOK[controller.User](t, e, http.MethodGet, "/v1/me", contributor, "", http.StatusOK)
	assert.True(t, domain.IsPublicID(first.PublicID), first.PublicID)
	assert.Equal(t, controller.User{
		ID:          endToEndContributorID,
		PublicID:    first.PublicID,
		DisplayName: domain.DefaultDisplayName(endToEndContributorID),
		Role:        domain.RoleContributor,
	}, first)

	// Renaming persists.
	renamed := sendOK[controller.User](t, e, http.MethodPatch, "/v1/me", contributor, `{"display_name":" Morwen "}`,
		http.StatusOK)
	assert.Equal(t, "Morwen", renamed.DisplayName)
	assert.Equal(t, renamed, sendOK[controller.User](t, e, http.MethodGet, "/v1/me", contributor, "", http.StatusOK))

	// With "Morwen" taken, the name is refused to another user, and so is the name with a blank letter appended,
	// which looks the same: that is an invalid name, and the message does not quote it.
	sendRejected(t, e, http.MethodPatch, "/v1/me", moderator, `{"display_name":"Morwen"}`,
		http.StatusConflict, httpjson.CodeDisplayNameTaken)
	rec := e.send(t, http.MethodPatch, "/v1/me", moderator, `{"display_name":"Morwen\u3164"}`)
	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	var blankLetter struct {
		Code    httpjson.ErrorCode `json:"code"`
		Message string             `json:"message"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &blankLetter))
	assert.Equal(t, httpjson.CodeInvalidRequest, blankLetter.Code)
	assert.NotContains(t, blankLetter.Message, "Morwen")

	// The contributor creates an API token, which authenticates as them but cannot manage tokens.
	created := sendOK[controller.CreatedAPIToken](t, e, http.MethodPost, "/v1/tokens", contributor,
		`{"name":"CI runner"}`, http.StatusCreated)
	apiAuth := "Bearer " + created.Token
	assert.Equal(t, renamed, sendOK[controller.User](t, e, http.MethodGet, "/v1/me", apiAuth, "", http.StatusOK))
	sendRejected(t, e, http.MethodGet, "/v1/tokens", apiAuth, "", http.StatusForbidden, httpjson.CodeAPIAuthNotAllowed)

	listed := sendOK[controller.APITokenListResponse](t, e, http.MethodGet, "/v1/tokens", contributor, "", http.StatusOK)
	require.Len(t, listed.Data, 1)
	assert.Equal(t, created.ID, listed.Data[0].ID)
	assert.Equal(t, created.Prefix, listed.Data[0].Prefix)
	assert.Equal(t, created.Name, listed.Data[0].Name)
	assert.Equal(t, created.CreatedAt, listed.Data[0].CreatedAt, "the creation time returned is the one stored")
	assert.NotNil(t, listed.Data[0].LastUsedAt, "using the token records its last use")

	// The bootstrapped moderator blocks the contributor, which blocks both of their credentials everywhere.
	sendRejected(t, e, http.MethodPut, endToEndContributorPath, contributor, `{"blocked":true}`,
		http.StatusForbidden, httpjson.CodeModeratorRequired)
	blocked := sendOK[controller.User](t, e, http.MethodPut, endToEndContributorPath, moderator, `{"blocked":true}`,
		http.StatusOK)
	assert.Equal(t, renamed, blocked)
	for _, authorization := range []string{contributor, apiAuth} {
		sendRejected(t, e, http.MethodGet, "/v1/me", authorization, "", http.StatusForbidden, httpjson.CodeUserBlocked)
		sendRejected(t, e, http.MethodPatch, "/v1/me", authorization, `{"display_name":"Blocked"}`,
			http.StatusForbidden, httpjson.CodeUserBlocked)
	}
	sendRejected(t, e, http.MethodPost, "/v1/tokens", contributor, `{}`, http.StatusForbidden, httpjson.CodeUserBlocked)

	// Unblocking restores both credentials.
	sendOK[controller.User](t, e, http.MethodPut, endToEndContributorPath, moderator, `{"blocked":false}`,
		http.StatusOK)
	assert.Equal(t, renamed, sendOK[controller.User](t, e, http.MethodGet, "/v1/me", apiAuth, "", http.StatusOK))
	assert.Equal(t, renamed, sendOK[controller.User](t, e, http.MethodGet, "/v1/me", contributor, "", http.StatusOK))

	// Moderators cannot be blocked, including by themselves.
	sendRejected(t, e, http.MethodPut, "/v1/users/facebook%7Ce2e-moderator/blocked", moderator, `{"blocked":true}`,
		http.StatusConflict, httpjson.CodeConflict)

	// Revoking the token stops it authenticating; revoking again still succeeds.
	revokePath := "/v1/tokens/" + created.ID
	for range 2 {
		rec := e.send(t, http.MethodDelete, revokePath, contributor, "")
		assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	}
	sendRejected(t, e, http.MethodGet, "/v1/me", apiAuth, "", http.StatusUnauthorized, httpjson.CodeUnauthenticated)
	listed = sendOK[controller.APITokenListResponse](t, e, http.MethodGet, "/v1/tokens", contributor, "", http.StatusOK)
	assert.Empty(t, listed.Data)

	// Restarting without the moderator's ID in the bootstrap list does not demote them.
	_, err = bootstrap.Execute(t.Context(), nil)
	require.NoError(t, err)
	assert.Equal(t, domain.RoleModerator,
		sendOK[controller.User](t, e, http.MethodGet, "/v1/me", moderator, "", http.StatusOK).Role)

	// Bootstrapping a blocked contributor unblocks them: moderators cannot be blocked, so a blocked moderator
	// could never be unblocked through the API.
	sendOK[controller.User](t, e, http.MethodPut, endToEndContributorPath, moderator, `{"blocked":true}`,
		http.StatusOK)
	_, err = bootstrap.Execute(t.Context(), []string{endToEndModeratorID, endToEndContributorID})
	require.NoError(t, err)
	assert.Equal(t, domain.RoleModerator,
		sendOK[controller.User](t, e, http.MethodGet, "/v1/me", contributor, "", http.StatusOK).Role)
}
