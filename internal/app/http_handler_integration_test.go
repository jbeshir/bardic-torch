//go:build integration

package app

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/auth0test"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
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

// endToEnd sends requests to the production handler, checking each request and response against the spec.
type endToEnd struct {
	handler  http.Handler
	contract *openapitest.Validator
}

// send serves a request with the given Authorization header value and JSON body, either of which may be empty.
func (e endToEnd) send(t *testing.T, method, path, authorization, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
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
// validator (against a local issuer).
func TestHTTP_EndToEnd(t *testing.T) {
	t.Parallel()

	db := mysqltest.NewDatabase(t)
	kettle := catalogtest.New(t, db).Piece(catalogtest.Piece{Doc: domain.PieceDocument{Title: "The Copper Kettle"}})
	repository := mysql.New(db)
	bootstrap := &command.BootstrapModerators{Users: repository, Now: now}
	_, err := bootstrap.Execute(t.Context(), []string{endToEndModeratorID})
	require.NoError(t, err)

	issuer := auth0test.NewIssuer(t)
	auth0Validator, err := router.NewAuth0Validator(issuer.URL, endToEndAudience)
	require.NoError(t, err)
	authValidators := []router.AuthValidator{auth0Validator}
	e := endToEnd{handler: NewHTTPHandler(repositoryHTTPDependencies(repository, authValidators)), contract: openapitest.New(t)}

	auth0 := func(userID string) string {
		return "Bearer " + auth0test.Sign(t, issuer.Key, issuer.Claims(userID, endToEndAudience))
	}
	contributor, moderator := auth0(endToEndContributorID), auth0(endToEndModeratorID)

	sendOK[controller.HealthStatus](t, e, http.MethodGet, "/readyz", "", "", http.StatusOK)

	// Invalid Auth0 tokens for the contributor get the spec's 401, and do not create them.
	expiredClaims := issuer.Claims(endToEndContributorID, endToEndAudience)
	expiredClaims["exp"] = time.Now().Add(-time.Hour).Unix()
	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	for name, authorization := range map[string]string{
		"expired": "Bearer " + auth0test.Sign(t, issuer.Key, expiredClaims),
		"wrong_audience": "Bearer " +
			auth0test.Sign(t, issuer.Key, issuer.Claims(endToEndContributorID, "https://other.example")),
		"bad_signature": "Bearer " +
			auth0test.Sign(t, otherKey, issuer.Claims(endToEndContributorID, endToEndAudience)),
		// Over the validator's 8 KiB token cap.
		"oversized": "Bearer " + strings.Repeat("a", 9<<10),
	} {
		rec := e.send(t, http.MethodGet, "/v1/me", authorization, "")
		assert.Equal(t, http.StatusUnauthorized, rec.Code, name)
		assert.JSONEq(t, `{"code":"unauthenticated","message":"invalid credentials"}`, rec.Body.String(), name)

		// Public catalogue reads skip authentication, so the real validator never sees these tokens there.
		rec = e.send(t, http.MethodGet, "/v1/pieces", authorization, "")
		assert.Equal(t, http.StatusOK, rec.Code, name)
	}

	// The first authenticated request creates the user.
	assert.Equal(t, controller.User{
		ID:          endToEndContributorID,
		DisplayName: domain.DefaultDisplayName(endToEndContributorID),
		Role:        domain.RoleContributor,
	}, sendOK[controller.User](t, e, http.MethodGet, "/v1/me", contributor, "", http.StatusOK))

	// Renaming persists.
	renamed := sendOK[controller.User](t, e, http.MethodPatch, "/v1/me", contributor, `{"display_name":" Morwen "}`,
		http.StatusOK)
	assert.Equal(t, "Morwen", renamed.DisplayName)
	assert.Equal(t, renamed, sendOK[controller.User](t, e, http.MethodGet, "/v1/me", contributor, "", http.StatusOK))

	// The bootstrapped moderator blocks the contributor.
	sendRejected(t, e, http.MethodPut, endToEndContributorPath, contributor, `{"blocked":true}`,
		http.StatusForbidden, httpjson.CodeModeratorRequired)
	blocked := sendOK[controller.User](t, e, http.MethodPut, endToEndContributorPath, moderator, `{"blocked":true}`,
		http.StatusOK)
	assert.Equal(t, renamed, blocked)
	sendRejected(t, e, http.MethodGet, "/v1/me", contributor, "", http.StatusForbidden, httpjson.CodeUserBlocked)
	sendRejected(t, e, http.MethodPatch, "/v1/me", contributor, `{"display_name":"Blocked"}`,
		http.StatusForbidden, httpjson.CodeUserBlocked)

	// Unblocking restores them.
	sendOK[controller.User](t, e, http.MethodPut, endToEndContributorPath, moderator, `{"blocked":false}`,
		http.StatusOK)
	assert.Equal(t, renamed, sendOK[controller.User](t, e, http.MethodGet, "/v1/me", contributor, "", http.StatusOK))

	// Moderators cannot be blocked, including by themselves.
	sendRejected(t, e, http.MethodPut, "/v1/users/facebook%7Ce2e-moderator/blocked", moderator, `{"blocked":true}`,
		http.StatusConflict, httpjson.CodeConflict)

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

	revisionsEndToEnd(t, e, auth0("google-oauth2|e2e-editor"), moderator, kettle)
}

// revisionsEndToEnd walks a contributor's edit of the piece kettle through moderation: the contributor edits its
// editable document, the moderator finds the revision in the queue and approves it, and the public page shows the
// change; a second edit is rejected, and the contributor sees the note on it and in their list.
func revisionsEndToEnd(t *testing.T, e endToEnd, contributor, moderator string, kettle catalogtest.Ref) {
	t.Helper()

	type editable struct {
		BaseRevisionID int64           `json:"base_revision_id"`
		Document       json.RawMessage `json:"document"`
	}
	type view struct {
		ID         int64   `json:"id"`
		Status     string  `json:"status"`
		ReviewNote *string `json:"review_note"`
	}
	submit := func(notes string) view {
		ed := sendOK[editable](t, e, http.MethodGet, "/v1/pieces/"+kettle.Slug+"/editable", contributor, "",
			http.StatusOK)
		var doc domain.EditorPieceDocument
		require.NoError(t, json.Unmarshal(ed.Document, &doc))
		doc.NotesMD = &notes
		body, err := json.Marshal(map[string]any{"entity_type": "piece", "base_revision_id": ed.BaseRevisionID,
			"summary": "Add notes.", "document": doc, "acknowledge_duplicates": false, "acknowledge_stale": false})
		require.NoError(t, err)
		return sendOK[view](t, e, http.MethodPost, "/v1/revisions", contributor, string(body), http.StatusCreated)
	}

	approved := submit("Sung at the end of the night.")
	assert.Equal(t, "pending", approved.Status)
	queue := sendOK[struct {
		Data []struct {
			ID     int64 `json:"id"`
			Author struct {
				UserID string `json:"user_id"`
			} `json:"author"`
		} `json:"data"`
	}](t, e, http.MethodGet, "/v1/moderation/queue", moderator, "", http.StatusOK)
	require.Len(t, queue.Data, 1)
	assert.Equal(t, approved.ID, queue.Data[0].ID)
	assert.Equal(t, "google-oauth2|e2e-editor", queue.Data[0].Author.UserID)
	id := strconv.FormatInt(approved.ID, 10)
	sendOK[map[string]any](t, e, http.MethodGet, "/v1/moderation/revisions/"+id, moderator, "", http.StatusOK)
	rec := e.send(t, http.MethodPost, "/v1/revisions/"+id+"/approve", moderator, `{"acknowledge_stale":false}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	page := sendOK[struct {
		NotesMD string `json:"notes_md"`
	}](t, e, http.MethodGet, "/v1/pieces/"+kettle.Slug, "", "", http.StatusOK)
	assert.Equal(t, "Sung at the end of the night.", page.NotesMD)

	rejected := submit("Sung at dawn.")
	rejectedPath := "/revisions/" + strconv.FormatInt(rejected.ID, 10)
	rec = e.send(t, http.MethodPost, "/v1"+rejectedPath+"/reject", moderator, `{"note":"Please cite a source."}`)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	mine := sendOK[view](t, e, http.MethodGet, "/v1/me"+rejectedPath, contributor, "", http.StatusOK)
	assert.Equal(t, "rejected", mine.Status)
	assert.Equal(t, new("Please cite a source."), mine.ReviewNote)
	list := sendOK[struct {
		Data []view `json:"data"`
	}](t, e, http.MethodGet, "/v1/me/revisions?status=rejected", contributor, "", http.StatusOK)
	assert.Equal(t, []view{mine}, list.Data)
}

const (
	healthzPath     = "/healthz"
	readyzPath      = "/readyz"
	mePath          = "/v1/me"
	piecesPath      = "/v1/pieces"
	personsPath     = "/v1/persons"
	termsPath       = "/v1/terms"
	collectionsPath = "/v1/collections"
	eventsPath      = "/v1/events"
	needsWorkPath   = "/v1/needs-work"
)

// errorResponse is the JSON body of an error response.
type errorResponse struct {
	Code httpjson.ErrorCode `json:"code"`
}
