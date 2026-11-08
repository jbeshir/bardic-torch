//go:build integration

package app

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
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

// revisionWorld is the production handler over a private database, authenticating with the real Auth0 validator
// (against a local issuer) and the real API token validator.
type revisionWorld struct {
	e          endToEnd
	db         *sql.DB
	repository *mysql.Repository
	issuer     auth0test.Issuer
}

// newRevisionWorld returns a revisionWorld over a fresh database in which moderatorID is a bootstrapped moderator.
// If logger is not nil, the handler logs every request to it.
func newRevisionWorld(t *testing.T, logger *slog.Logger, moderatorID string) revisionWorld {
	t.Helper()

	db := mysqltest.NewDatabase(t)
	bootstrap := &command.BootstrapModerators{Promoter: mysql.New(db), Now: now}
	_, err := bootstrap.Execute(t.Context(), []string{moderatorID})
	require.NoError(t, err)

	return revisionWorldOn(t, db, logger)
}

// revisionWorldOn returns a revisionWorld over db, a migrated database, promoting no one. If logger is not nil,
// the handler logs every request to it.
func revisionWorldOn(t *testing.T, db *sql.DB, logger *slog.Logger) revisionWorld {
	t.Helper()

	repository := mysql.New(db)
	issuer := auth0test.NewIssuer(t)
	auth0Validator, err := router.NewAuth0Validator(issuer.URL, endToEndAudience)
	require.NoError(t, err)
	authValidators := []router.AuthValidator{auth0Validator, router.NewAPITokenValidator(repository, repository, now)}

	return revisionWorld{
		e: endToEnd{handler: NewHTTPHandler(repositoryHTTPDependencies(repository, authValidators, passingTurnstile{}, unlimitedRateLimits())),
			contract: openapitest.New(t), logger: logger},
		db: db, repository: repository, issuer: issuer,
	}
}

// auth0 returns the Authorization header value of a valid Auth0 access token of userID, who exists once a request
// with it has been served.
func (w revisionWorld) auth0(t *testing.T, userID string) string {
	t.Helper()

	return "Bearer auth0|" + auth0test.Sign(t, w.issuer.Key, w.issuer.Claims(userID, endToEndAudience))
}

// apiToken creates an API token of the user authorization authenticates, returning its Authorization header value.
func (w revisionWorld) apiToken(t *testing.T, authorization string) string {
	t.Helper()

	created := sendOK[controller.CreatedAPIToken](t, w.e, http.MethodPost, tokensPath, authorization,
		`{"name":"Revision runner"}`, http.StatusCreated)
	return "Bearer " + created.Token
}

// block has moderator block the user with userID, whose "|" blockedPath gives percent-encoded.
func (w revisionWorld) block(t *testing.T, moderator, blockedPath string) {
	t.Helper()

	sendOK[controller.User](t, w.e, http.MethodPut, "/v1/users/"+blockedPath+"/blocked", moderator,
		`{"blocked":true}`, http.StatusOK)
}

// Editor document fields the tests edit.
const (
	nameField        = "name"
	statusField      = "status"
	notesField       = "notes_md"
	descriptionField = "description_md"
)

// jsonObject is a decoded JSON object of a response, for the fields a test reads or edits.
type jsonObject = map[string]any

// encodeJSON returns v's JSON encoding.
func encodeJSON(t *testing.T, v any) string {
	t.Helper()

	encoded, err := json.Marshal(v)
	require.NoError(t, err)
	return string(encoded)
}

// objectID returns the integer field key of o, such as a revision's id.
func objectID(t *testing.T, o jsonObject, key string) int64 {
	t.Helper()

	id, ok := o[key].(float64)
	require.True(t, ok, "%s is not a number: %v", key, o[key])
	return int64(id)
}

// idPath is prefix followed by id.
func idPath(prefix string, id int64) string {
	return prefix + strconv.FormatInt(id, 10)
}

// entitySlug addresses an entity by slug in a draft request.
func entitySlug(slug string) jsonObject {
	return jsonObject{"slug": slug, collectionIDParam: nil}
}

// draftRequest is the body of POST /v1/drafts: a draft of entity (nil for a new entity) on base (nil for the
// current revision, or a new entity).
func draftRequest(t *testing.T, entityType domain.EntityType, entity jsonObject, base any, document jsonObject,
	summary string,
) string {
	t.Helper()

	var key any
	if entity != nil {
		key = entity
	}
	return encodeJSON(t, jsonObject{entityTypeField: entityType, "entity": key, "base_revision_id": base,
		documentField: document, "summary": summary})
}

// saveDraft saves a draft as authorization, returning it.
func saveDraft(t *testing.T, e endToEnd, authorization, body string) jsonObject {
	t.Helper()

	return sendOK[jsonObject](t, e, http.MethodPost, draftsPath, authorization, body, http.StatusCreated)
}

// submitBody is the body of POST /v1/revisions submitting draftID.
func submitBody(draftID int64) string {
	return `{"draft_id":` + strconv.FormatInt(draftID, 10) + `,"acknowledge_duplicates":false,"acknowledged_flags":[]}`
}

// submitDraft submits draftID as authorization, returning the revision.
func submitDraft(t *testing.T, e endToEnd, authorization string, draftID int64) jsonObject {
	t.Helper()

	return sendOK[jsonObject](t, e, http.MethodPost, revisionsPath, authorization, submitBody(draftID),
		http.StatusCreated)
}

// editablePath is the path of the editable document of the entity collection/key.
func editablePath(collection, key string) string {
	return "/v1/" + collection + "/" + key + "/editable"
}

// pendingPath is the path of the caller's pending revision of the entity collection/key.
func pendingPath(collection, key string) string {
	return "/v1/" + collection + "/" + key + "/pending"
}

// editedDocument returns the document of the editable response editable with the fields of changes set.
func editedDocument(t *testing.T, editable jsonObject, changes jsonObject) jsonObject {
	t.Helper()

	document, ok := editable["document"].(jsonObject)
	require.True(t, ok, "the editable response has no document")
	edited := make(jsonObject, len(document))
	for k, v := range document {
		edited[k] = v
	}
	for k, v := range changes {
		edited[k] = v
	}
	return edited
}

const (
	revisionE2EModeratorID   = "facebook|revision-e2e-moderator"
	revisionE2EContributorID = "google-oauth2|revision-e2e-contributor"
	revisionE2EScriptUserID  = "google-oauth2|revision-e2e-script-user"
	revisionE2EBlockedID     = "google-oauth2|revision-e2e-blocked"
	revisionE2EBlockedPath   = "google-oauth2%7Crevision-e2e-blocked"
)

// TestHTTP_RevisionEndToEnd runs a contributor's whole revision flow through the production handler over a real
// database, once with an Auth0 session and once with an API token, and checks that a blocked contributor is
// refused every step of it.
func TestHTTP_RevisionEndToEnd(t *testing.T) {
	t.Parallel()

	w := newRevisionWorld(t, nil, revisionE2EModeratorID)
	moderator := w.auth0(t, revisionE2EModeratorID)
	c := catalogtest.New(t, w.db)
	imported := func(name, hiddenAlias string) catalogtest.Ref {
		return c.Person(catalogtest.Person{Doc: domain.PersonDocument{Name: name, Aliases: []domain.DocPersonAlias{
			{Name: hiddenAlias, Kind: domain.PersonAliasKindLegal},
		}}})
	}

	t.Run("auth0_contributor", func(t *testing.T) {
		person := imported("Corwin Placeholder", "Corwin Hidden Legal Name")
		contributeToPerson(t, w, w.auth0(t, revisionE2EContributorID), moderator, person, "Corwin Hidden Legal Name")
	})
	t.Run("api_token_contributor", func(t *testing.T) {
		person := imported("Tamsin Placeholder", "Tamsin Hidden Legal Name")
		contributeToPerson(t, w, w.apiToken(t, w.auth0(t, revisionE2EScriptUserID)), moderator, person,
			"Tamsin Hidden Legal Name")
	})
	t.Run("status_change_without_source_is_422_source_required", func(t *testing.T) {
		person := imported("Isolde Placeholder", "Isolde Hidden Legal Name")
		contributor := w.auth0(t, revisionE2EContributorID)
		editable := sendOK[jsonObject](t, w.e, http.MethodGet, editablePath("persons", person.Slug), contributor, "",
			http.StatusOK)
		draft := saveDraft(t, w.e, contributor, draftRequest(t, domain.EntityTypePerson, entitySlug(person.Slug),
			person.RevisionID, editedDocument(t, editable, jsonObject{statusField: string(domain.PersonStatusDeceased)}),
			"Mark deceased."))

		rec := w.e.send(t, http.MethodPost, revisionsPath, contributor, submitBody(objectID(t, draft, "id")))

		require.Equal(t, http.StatusUnprocessableEntity, rec.Code, rec.Body.String())
		var body struct {
			Code     httpjson.ErrorCode `json:"code"`
			Problems []struct {
				Path    string `json:"path"`
				Problem string `json:"problem"`
			} `json:"problems"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, httpjson.CodeInvalidDocument, body.Code)
		require.Len(t, body.Problems, 1)
		assert.Equal(t, "/status_source_url", body.Problems[0].Path)
		assert.Equal(t, string(domain.ProblemSourceRequired), body.Problems[0].Problem)
	})
	t.Run("blocked_contributor", func(t *testing.T) {
		person := imported("Blocked Placeholder", "Blocked Hidden Legal Name")
		requireBlockedEverywhere(t, w, moderator, person)
	})
}

// contributeToPerson runs a contributor's revision flow on person, imported with the hidden legal alias
// hiddenAlias: editable (the alias withheld), draft, submit, pending; withdraw and submit again; a superseding
// submit on the old base after a moderator renamed the person; and its approval, which keeps the moderator's rename.
func contributeToPerson(
	t *testing.T, w revisionWorld, contributor, moderator string, person catalogtest.Ref, hiddenAlias string,
) {
	t.Helper()
	e := w.e
	editable := editablePath("persons", person.Slug)

	// The editable document of the imported person withholds the hidden alias.
	start := sendOK[jsonObject](t, e, http.MethodGet, editable, contributor, "", http.StatusOK)
	assert.Equal(t, string(domain.StartingPointCurrent), start["starting_point"])
	assert.Equal(t, person.RevisionID, objectID(t, start, "base_revision_id"))
	rec := e.send(t, http.MethodGet, editable, contributor, "")
	assert.NotContains(t, rec.Body.String(), hiddenAlias)

	draft := saveDraft(t, e, contributor, draftRequest(t, domain.EntityTypePerson, entitySlug(person.Slug),
		person.RevisionID, editedDocument(t, start, jsonObject{notesField: "Sings at revels."}), "Add notes."))
	first := submitDraft(t, e, contributor, objectID(t, draft, "id"))
	assert.Equal(t, string(domain.RevisionStatusPending), first[statusField])
	assert.Equal(t, string(domain.RevisionSourceUser), first["source"])
	firstID := objectID(t, first, "id")

	pending := sendOK[jsonObject](t, e, http.MethodGet, pendingPath("persons", person.Slug), contributor, "",
		http.StatusOK)
	assert.Equal(t, firstID, objectID(t, pending, "id"))
	assert.Contains(t, encodeJSON(t, pending["snapshot"]), "Sings at revels.")

	// The public read still shows the approved version.
	rec = e.send(t, http.MethodGet, "/v1/persons/"+person.Slug, "", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.NotContains(t, rec.Body.String(), "Sings at revels.")

	// Withdrawing leaves nothing pending; submitting again does.
	withdrawn := sendOK[jsonObject](t, e, http.MethodPost, idPath("/v1/revisions/", firstID)+"/withdraw",
		contributor, "", http.StatusOK)
	assert.Equal(t, string(domain.RevisionStatusWithdrawn), withdrawn[statusField])
	sendRejected(t, e, http.MethodGet, pendingPath("persons", person.Slug), contributor, "", http.StatusNotFound,
		httpjson.CodeNotFound)
	draft = saveDraft(t, e, contributor, draftRequest(t, domain.EntityTypePerson, entitySlug(person.Slug),
		person.RevisionID, editedDocument(t, start, jsonObject{notesField: "Sings at revels."}), "Add notes."))
	second := submitDraft(t, e, contributor, objectID(t, draft, "id"))
	assert.Equal(t, string(domain.RevisionStatusPending), second[statusField])
	secondID := objectID(t, second, "id")

	// Re-opened while pending, the editable document starts from the pending revision, on its submitted base.
	reopened := sendOK[jsonObject](t, e, http.MethodGet, editable, contributor, "", http.StatusOK)
	assert.Equal(t, string(domain.StartingPointPending), reopened["starting_point"])
	assert.Equal(t, person.RevisionID, objectID(t, reopened, "base_revision_id"))
	reopenedPending, ok := reopened["pending"].(jsonObject)
	require.True(t, ok, "the editable document names the pending revision")
	assert.Equal(t, secondID, objectID(t, reopenedPending, "id"))
	assert.Equal(t, "Sings at revels.", editedDocument(t, reopened, nil)[notesField])

	// Meanwhile a moderator renames the person, which is approved at once.
	const renamed = "Renamed By Moderator"
	moderatorStart := sendOK[jsonObject](t, e, http.MethodGet, editable, moderator, "", http.StatusOK)
	moderatorDraft := saveDraft(t, e, moderator, draftRequest(t, domain.EntityTypePerson, entitySlug(person.Slug),
		person.RevisionID, editedDocument(t, moderatorStart, jsonObject{nameField: renamed}), "Use the stage name."))
	moderatorEdit := submitDraft(t, e, moderator, objectID(t, moderatorDraft, "id"))
	assert.Equal(t, string(domain.RevisionStatusApproved), moderatorEdit[statusField])

	// The contributor's superseding submit is pending on the base they edited, not the moderator's revision.
	draft = saveDraft(t, e, contributor, draftRequest(t, domain.EntityTypePerson, entitySlug(person.Slug),
		objectID(t, reopened, "base_revision_id"),
		editedDocument(t, reopened, jsonObject{notesField: "Sings at revels and feasts."}), "More notes."))
	third := submitDraft(t, e, contributor, objectID(t, draft, "id"))
	assert.Equal(t, string(domain.RevisionStatusPending), third[statusField])
	assert.Equal(t, person.RevisionID, objectID(t, third, "base_revision_id"))
	thirdID := objectID(t, third, "id")
	superseded := sendOK[jsonObject](t, e, http.MethodGet, idPath("/v1/me/revisions/", secondID), contributor, "",
		http.StatusOK)
	assert.Equal(t, string(domain.RevisionStatusWithdrawn), superseded[statusField])

	// Approved (moderator HTTP is not there yet, so through the repository), the stale revision is merged: the
	// moderator's rename survives next to the contributor's notes, and the hidden alias stays stored and hidden.
	require.NoError(t, w.repository.ApproveRevision(t.Context(), datasources.ApproveRequest{
		Actor:      domain.Actor{UserID: revisionE2EModeratorID, Role: domain.RoleModerator},
		RevisionID: thirdID, Now: now(),
	}))
	public := sendOK[jsonObject](t, e, http.MethodGet, "/v1/persons/"+person.Slug, "", "", http.StatusOK)
	assert.Equal(t, renamed, public[nameField])
	assert.Contains(t, encodeJSON(t, public), "Sings at revels and feasts.")
	assert.NotContains(t, encodeJSON(t, public), hiddenAlias)
	var hidden int
	require.NoError(t, w.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM person_aliases "+
		"WHERE person_id = ? AND name = ? AND NOT is_public", person.ID, hiddenAlias).Scan(&hidden))
	assert.Equal(t, 1, hidden)
	approved := sendOK[jsonObject](t, e, http.MethodGet, idPath("/v1/me/revisions/", thirdID), contributor, "",
		http.StatusOK)
	assert.Equal(t, string(domain.RevisionStatusApproved), approved[statusField])
	sendRejected(t, e, http.MethodGet, pendingPath("persons", person.Slug), contributor, "", http.StatusNotFound,
		httpjson.CodeNotFound)
}

// requireBlockedEverywhere blocks a contributor who has a draft and a pending revision of person, and checks that
// both of their credentials get 403 user_blocked on every endpoint of the flow.
func requireBlockedEverywhere(t *testing.T, w revisionWorld, moderator string, person catalogtest.Ref) {
	t.Helper()
	e := w.e

	auth0 := w.auth0(t, revisionE2EBlockedID)
	token := w.apiToken(t, auth0)
	start := sendOK[jsonObject](t, e, http.MethodGet, editablePath("persons", person.Slug), auth0, "", http.StatusOK)
	document := editedDocument(t, start, jsonObject{notesField: "Blocked notes."})
	draftBody := draftRequest(t, domain.EntityTypePerson, entitySlug(person.Slug), person.RevisionID, document,
		"Blocked edit.")
	pendingID := objectID(t, submitDraft(t, e, auth0, objectID(t, saveDraft(t, e, auth0, draftBody), "id")), "id")
	draftID := objectID(t, saveDraft(t, e, token, draftRequest(t, domain.EntityTypePiece, nil, nil,
		newPieceDocument("Blocked Draft Title"), "A new piece.")), "id")
	w.block(t, moderator, revisionE2EBlockedPath)

	updateBody := encodeJSON(t, jsonObject{"entity_type": domain.EntityTypePiece,
		"document": newPieceDocument("Blocked Draft Title"), "summary": "A new piece."})
	for _, authorization := range []string{auth0, token} {
		for _, req := range []struct{ method, path, body string }{
			{http.MethodGet, editablePath("persons", person.Slug), ""},
			{http.MethodGet, pendingPath("persons", person.Slug), ""},
			{http.MethodPost, draftsPath, draftBody},
			{http.MethodGet, idPath("/v1/drafts/", draftID), ""},
			{http.MethodPut, idPath("/v1/drafts/", draftID), updateBody},
			{http.MethodDelete, idPath("/v1/drafts/", draftID), ""},
			{http.MethodPost, revisionsPath, submitBody(draftID)},
			{http.MethodPost, idPath("/v1/revisions/", pendingID) + "/withdraw", ""},
			{http.MethodPost, idPath("/v1/revisions/", person.RevisionID) + "/revert",
				`{"summary":"Undo.","acknowledged_flags":[]}`},
			{http.MethodGet, myRevisionsPath, ""},
			{http.MethodGet, idPath("/v1/me/revisions/", pendingID), ""},
		} {
			sendRejected(t, e, req.method, req.path, authorization, req.body, http.StatusForbidden,
				httpjson.CodeUserBlocked)
		}
	}
}

// newPieceDocument is the editor document of a new song titled title, with every other field empty or default.
func newPieceDocument(title string) jsonObject {
	return jsonObject{
		titleField: title, "kind": domain.PieceKindSong, "secondary_kinds": []any{},
		"conservation": jsonObject{statusField: domain.ConservationStatusUnassessed, "source_url": nil},
		"is_stub":      false, "notes_md": nil, "version_notes_md": nil, "licensing_status": domain.LicensingStatusUnknown,
		"licensing_notes_md": nil, "licence": nil, "historical_source_md": nil, "period_start": nil,
		"period_end": nil, "aliases": []any{}, "terms": []any{}, "relations": []any{}, "credits": []any{},
		"links": []any{},
	}
}

// TestHTTP_RevisionSnapshotNestedSummaryIsCurrent pins what a nested summary in a revision snapshot shows: the
// referenced piece as it is now, not as it was when the revision was saved.
func TestHTTP_RevisionSnapshotNestedSummaryIsCurrent(t *testing.T) {
	t.Parallel()

	w := newRevisionWorld(t, nil, "auth0|snapshot-moderator")
	c := catalogtest.New(t, w.db)
	brass := c.Piece(catalogtest.Piece{Slug: "the-brass-kettle", Doc: domain.PieceDocument{Title: "The Brass Kettle"}})
	kettle := c.Piece(catalogtest.Piece{Slug: "the-copper-kettle", Doc: domain.PieceDocument{
		Title:     "The Copper Kettle",
		Relations: []domain.DocPieceRelation{{ToPieceID: brass.ID, Type: domain.PieceRelationTypeVariantOf}},
	}})
	c.Exec("UPDATE pieces SET conservation_status = 'endangered' WHERE id = ?", brass.ID)

	revision := sendOK[jsonObject](t, w.e, http.MethodGet, idPath("/v1/revisions/", kettle.RevisionID), "", "",
		http.StatusOK)
	snapshot, ok := revision["snapshot"].(jsonObject)
	require.True(t, ok, "the revision has a snapshot")
	relations, ok := snapshot["relations"].([]any)
	require.True(t, ok, "the snapshot has relations")
	require.Len(t, relations, 1)
	piece, ok := relations[0].(jsonObject)["piece"].(jsonObject)
	require.True(t, ok, "the relation has a piece")
	assert.Equal(t, string(domain.ConservationStatusEndangered), piece["conservation_status"],
		"the related piece's status now, not when the revision was saved")
}
