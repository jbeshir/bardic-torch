//go:build integration

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// revisionViewer is who sends a revision leak matrix request.
type revisionViewer string

const (
	// viewerAuthor is contributor A, the author of every revision the matrix reads.
	viewerAuthor revisionViewer = "A"
	// viewerOther is contributor B.
	viewerOther revisionViewer = "B"
	// viewerModerator is moderator M in an Auth0 session.
	viewerModerator revisionViewer = "M"
	// viewerModeratorToken is the same moderator with an API token, a contributor (D25).
	viewerModeratorToken revisionViewer = "MT"
)

// The users of the revision leak matrix. None of their IDs is marked, since request logs carry user IDs.
const (
	revisionLeakAuthorID    = "google-oauth2|revision-leak-author"
	revisionLeakOtherID     = "google-oauth2|revision-leak-other"
	revisionLeakSuggesterID = "google-oauth2|revision-leak-suggester"
	revisionLeakModeratorID = "facebook|revision-leak-moderator"
	revisionLeakBlockedID   = "google-oauth2|revision-leak-blocked"
	revisionLeakBlockedPath = "google-oauth2%7Crevision-leak-blocked"
)

// The marked strings of the revision leak fixture.
const (
	// wrenLegalAlias and wrenSecondAlias are hidden legal aliases of wren.
	wrenLegalAlias  = "leakmark-legal-alias"
	wrenSecondAlias = "Leakmark Second Legal Alias"
	// hiddenLaterURL is a link of wren's that A receives in their draft, hidden after the draft is saved.
	hiddenLaterURL = "https://example.org/leakmark-hidden-later"
	// typedLinkURL is hiddenLinkURL in another spelling, which A types into their revision of wren.
	typedLinkURL = "https://EXAMPLE.org/leakmark-hidden-link"
	// suggestedTermName is the name of a pending new term of another contributor's, which the piece references.
	suggestedTermName = "Leakmark Suggested Term"
	// unapprovedRegionName is the unapproved region of the public collection.
	unapprovedRegionName = "Leakmark Unapproved Region"
	// A's own text.
	pendingNotes   = "leakmark-pending-notes"
	pendingSummary = "leakmark-pending-summary"
	draftTitle     = "leakmark-draft-title"
	pendingTerm    = "leakmark-pending-term"
	pendingEvent   = "leakmark-pending-event"
	// flaggedSummary names a hidden alias, so the revision is flagged hidden_alias_in_summary.
	flaggedSummary = "leakmark-flagged-summary, as " + wrenLegalAlias + " told me."
)

// flagCodes are the moderator flags, which no contributor-facing response may name.
var flagCodes = []string{
	"hidden_alias_in_summary", "hidden_alias_in_text", "hidden_alias_collision", "hidden_data_conflict",
	"hidden_link_collision", "hidden_link_attached", "possible_duplicate", "references_pending",
}

// revisionLeakFixture is what the revision leak matrix's requests address.
type revisionLeakFixture struct {
	// wren is a public person with hidden aliases and a hidden link; noPage prefers no page.
	wren, noPage catalogtest.Ref
	// piece credits wren and noPage, is tagged with another contributor's pending term and attaches the hidden link.
	piece catalogtest.Ref
	// westmarch is a public term, archive a public collection in an unapproved region, moot a public event.
	westmarch, archive, moot catalogtest.Ref
	// pendingTermSlug is the placeholder slug of A's pending new term.
	pendingTermSlug string
	// pieceDraft is A's draft of a new piece.
	pieceDraft int64
	// own are A's revisions, drafts included, with the marked strings of A's own each shows A.
	own []ownRevision
}

// ownRevision is one of A's revisions and the marked strings A typed that its author view shows.
type ownRevision struct {
	id      int64
	reveals []string
}

// revisionLeakRequest is a GET the revision leak matrix sends as viewer, and its expected status.
type revisionLeakRequest struct {
	viewer revisionViewer
	path   string
	status int
	// reveals are marked strings the response may show, each of which it must show.
	reveals []string
	// full skips the scan for a moderator's complete view; check then asserts the hidden data is there.
	full bool
	// check, if not nil, makes further assertions on the raw response body.
	check func(t *testing.T, body string)
}

// revisionLeakWorld is the handler over the revision leak fixture, with the viewers' credentials.
type revisionLeakWorld struct {
	revisionWorld
	viewers map[revisionViewer]string
	// blocked are the blocked user's Auth0 and API token credentials.
	blocked []string
	logs    *logCapture
}

// logCapture collects every log record the handler writes, as JSON lines.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logCapture) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.Write(p)
}

func (l *logCapture) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.buf.String()
}

// requireNoMarkedLogs checks that records were captured and that none of them holds a marked string.
func (l *logCapture) requireNoMarkedLogs(t *testing.T) {
	t.Helper()

	logs := l.String()
	require.Contains(t, logs, "request completed", "no request was logged, so this check proves nothing")
	for line := range strings.Lines(logs) {
		assert.NotContains(t, strings.ToLower(line), leakMarker, "a log record holds a marked string")
	}
}

// newRevisionLeakWorld seeds a private database with the revision leak fixture: catalogue rows through
// catalogtest, and every contributor's revision through the HTTP API, as its author would write it.
func newRevisionLeakWorld(t *testing.T) (revisionLeakWorld, revisionLeakFixture) {
	t.Helper()

	logs := &logCapture{}
	w := revisionLeakWorld{
		revisionWorld: newRevisionWorld(t,
			slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})),
			revisionLeakModeratorID),
		logs: logs,
	}
	w.viewers = map[revisionViewer]string{
		viewerAuthor: w.auth0(t, revisionLeakAuthorID), viewerOther: w.auth0(t, revisionLeakOtherID),
		viewerModerator: w.auth0(t, revisionLeakModeratorID),
	}
	for _, authorization := range w.viewers {
		sendOK[jsonObject](t, w.e, http.MethodGet, mePath, authorization, "", http.StatusOK)
	}
	w.viewers[viewerModeratorToken] = w.apiToken(t, w.viewers[viewerModerator])
	blocked := w.auth0(t, revisionLeakBlockedID)
	w.blocked = []string{blocked, w.apiToken(t, blocked)}
	w.block(t, w.viewers[viewerModerator], revisionLeakBlockedPath)

	fx := seedRevisionLeakCatalogue(t, w)
	fx = w.seedAuthorRevisions(t, fx)
	return w, fx
}

// seedRevisionLeakCatalogue seeds the public and hidden catalogue rows, with another contributor's pending new
// term referenced by the public piece.
func seedRevisionLeakCatalogue(t *testing.T, w revisionLeakWorld) revisionLeakFixture {
	t.Helper()

	c := catalogtest.New(t, w.db)
	lanterns := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetTopic, Name: "Lanterns"}})
	westmarch := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation, Name: "Westmarch"}})
	region := c.Term(catalogtest.Term{Doc: domain.TermDocument{Facet: domain.FacetAffiliation,
		Name: unapprovedRegionName}, Unapproved: true})
	archive := c.Collection(catalogtest.Collection{Doc: domain.CollectionDocument{Name: "Lantern Archive",
		RegionTermID: &region.ID}})
	moot := c.Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Lantern Moot"}})
	noPage := c.Person(catalogtest.Person{Doc: domain.PersonDocument{
		Name: noPageName, Status: domain.PersonStatusPrefersNoPage, NotesMD: new("Leakmark nopage notes"),
		Pronouns: new("leakmark/pronoun"),
		Aliases: []domain.DocPersonAlias{{Name: "Leakmark Nopage Public Alias", Kind: domain.PersonAliasKindStage,
			IsPublic: true}},
		AffiliationIDs: []int64{westmarch.ID},
	}})
	wren := c.Person(catalogtest.Person{Doc: domain.PersonDocument{
		Name: "Wren Placeholder",
		Aliases: []domain.DocPersonAlias{
			{Name: "Wren", Kind: domain.PersonAliasKindStage, IsPublic: true},
			{Name: wrenLegalAlias, Kind: domain.PersonAliasKindLegal},
			{Name: wrenSecondAlias, Kind: domain.PersonAliasKindLegal},
		},
		Links: []domain.DocPersonLink{
			{DocLink: domain.DocLink{URL: "https://example.org/wren", Roles: []domain.LinkRole{domain.LinkRoleWebsite}}},
			{DocLink: domain.DocLink{URL: hiddenLinkURL, Title: new("Leakmark Hidden Link"),
				Roles: []domain.LinkRole{domain.LinkRoleOther}}},
			{DocLink: domain.DocLink{URL: hiddenLaterURL, Roles: []domain.LinkRole{domain.LinkRoleOther}}},
		},
	}})

	suggester := w.auth0(t, revisionLeakSuggesterID)
	suggested := submitDraft(t, w.e, suggester, objectID(t, saveDraft(t, w.e, suggester, draftRequest(t,
		domain.EntityTypeTerm, nil, nil, newTermDocument(suggestedTermName), "Suggest a topic.")), "id"))
	var suggestedTermID int64
	require.NoError(t, w.db.QueryRowContext(t.Context(), "SELECT entity_id FROM revisions WHERE id = ?",
		objectID(t, suggested, "id")).Scan(&suggestedTermID))

	piece := c.Piece(catalogtest.Piece{Doc: domain.PieceDocument{
		Title: "The Lantern Song",
		Terms: []domain.DocPieceTerm{{TermID: lanterns.ID, IsPrimary: true}, {TermID: suggestedTermID}},
		Credits: []domain.DocCredit{
			namedCredit(wren.ID, domain.CreditRoleLyrics, nil),
			namedCredit(noPage.ID, domain.CreditRoleTune, new("Leakmark nopage credit note")),
		},
		Links: []domain.DocPieceLink{
			{DocLink: domain.DocLink{URL: "https://example.org/lantern", Roles: []domain.LinkRole{domain.LinkRoleText}}},
			{DocLink: domain.DocLink{URL: hiddenLinkURL, Roles: []domain.LinkRole{domain.LinkRoleLyrics}},
				Performances: []domain.DocPerformance{{EventID: &moot.ID,
					DescriptionVerbatim: "Leakmark hidden piece performance", PerformerIDs: []int64{wren.ID}}}},
		},
	}})
	c.SetLinkState(hiddenLinkURL, catalogtest.LinkState{IsHidden: true})

	return revisionLeakFixture{wren: wren, noPage: noPage, piece: piece, westmarch: westmarch, archive: archive,
		moot: moot}
}

// seedAuthorRevisions writes A's revisions through the API: pending edits of the piece, the event and the term
// (with a flagged summary), a pending new term, a draft of a new piece, the validation oracle's renames of wren and
// the provenance revision of wren.
func (w revisionLeakWorld) seedAuthorRevisions(t *testing.T, fx revisionLeakFixture) revisionLeakFixture {
	t.Helper()
	a := w.viewers[viewerAuthor]

	piece := w.submitEdit(t, "pieces", fx.piece.Slug, domain.EntityTypePiece, jsonObject{notesField: pendingNotes},
		pendingSummary)
	event := w.submitEdit(t, "events", fx.moot.Slug, domain.EntityTypeEvent,
		jsonObject{descriptionField: pendingEvent}, "Describe the moot.")
	flagged := w.submitEdit(t, "terms", fx.westmarch.Slug, domain.EntityTypeTerm,
		jsonObject{descriptionField: "Riders of the west march."}, flaggedSummary)
	requireFlagStored(t, w, flagged, "hidden_alias_in_summary")

	term := submitDraft(t, w.e, a, objectID(t, saveDraft(t, w.e, a, draftRequest(t, domain.EntityTypeTerm, nil, nil,
		newTermDocument(pendingTerm), "Suggest a topic.")), "id"))
	entity, ok := term["entity"].(jsonObject)
	require.True(t, ok, "the pending new term has an entity")
	fx.pendingTermSlug, ok = entity["slug"].(string)
	require.True(t, ok, "the pending new term has a placeholder slug")

	fx.pieceDraft = objectID(t, saveDraft(t, w.e, a, draftRequest(t, domain.EntityTypePiece, nil, nil,
		newPieceDocument(draftTitle), "A new piece.")), "id")

	renameToAlias, renameToFresh := w.validationOracle(t, fx)
	provenance := w.provenance(t, fx)

	fx.own = []ownRevision{
		{id: piece, reveals: []string{pendingNotes, pendingSummary}},
		{id: event, reveals: []string{pendingEvent}},
		{id: flagged, reveals: []string{flaggedSummary}},
		{id: objectID(t, term, "id"), reveals: []string{pendingTerm}},
		{id: fx.pieceDraft, reveals: []string{draftTitle}},
		{id: renameToAlias, reveals: []string{wrenLegalAlias}},
		{id: renameToFresh},
		{id: provenance, reveals: []string{wrenLegalAlias, typedLinkURL}},
	}
	return fx
}

// submitEdit has A edit the public entity collection/key from its editable document with changes, and submit it
// with summary, returning the pending revision's ID.
func (w revisionLeakWorld) submitEdit(t *testing.T, collection, key string, entityType domain.EntityType,
	changes jsonObject, summary string,
) int64 {
	t.Helper()
	a := w.viewers[viewerAuthor]

	start := sendOK[jsonObject](t, w.e, http.MethodGet, editablePath(collection, key), a, "", http.StatusOK)
	draft := saveDraft(t, w.e, a, draftRequest(t, entityType, entitySlug(key), start["base_revision_id"],
		editedDocument(t, start, changes), summary))
	revision := submitDraft(t, w.e, a, objectID(t, draft, "id"))
	require.Equal(t, string(domain.RevisionStatusPending), revision["status"])
	return objectID(t, revision, "id")
}

// requireFlagStored checks that the revision revisionID is stored with the moderator flag code, so that a row
// checking that the author never sees it tests something.
func requireFlagStored(t *testing.T, w revisionLeakWorld, revisionID int64, code string) {
	t.Helper()

	var flagged bool
	require.NoError(t, w.db.QueryRowContext(t.Context(), "SELECT COALESCE(JSON_CONTAINS(flags_json, "+
		"JSON_OBJECT('code', ?)), FALSE) FROM revisions WHERE id = ?", code, revisionID).Scan(&flagged))
	require.True(t, flagged, "revision %d is not flagged %s", revisionID, code)
}

// validationOracle is the B1 row: A renames wren to its hidden alias, and (after withdrawing that) to a fresh
// name. The two submits' responses and author views are byte-equal once the typed name, IDs and times are masked,
// and neither shows hidden_data_conflict, though the first is flagged with it. It returns both revisions' IDs.
func (w revisionLeakWorld) validationOracle(t *testing.T, fx revisionLeakFixture) (int64, int64) {
	t.Helper()
	a := w.viewers[viewerAuthor]

	start := sendOK[jsonObject](t, w.e, http.MethodGet, editablePath("persons", fx.wren.Slug), a, "", http.StatusOK)
	rename := func(name string) (int64, []string) {
		draft := saveDraft(t, w.e, a, draftRequest(t, domain.EntityTypePerson, entitySlug(fx.wren.Slug),
			start["base_revision_id"], editedDocument(t, start, jsonObject{nameField: name}), "Use the usual name."))
		submitted := w.e.send(t, http.MethodPost, revisionsPath, a, submitBody(objectID(t, draft, "id")))
		require.Equal(t, http.StatusCreated, submitted.Code, submitted.Body.String())
		var revision jsonObject
		require.NoError(t, json.Unmarshal(submitted.Body.Bytes(), &revision))
		id := objectID(t, revision, "id")
		view := w.e.send(t, http.MethodGet, idPath("/v1/me/revisions/", id), a, "")
		require.Equal(t, http.StatusOK, view.Code, view.Body.String())
		withdrawn := w.e.send(t, http.MethodPost, idPath("/v1/revisions/", id)+"/withdraw", a, "")
		require.Equal(t, http.StatusOK, withdrawn.Code, withdrawn.Body.String())

		return id, []string{submitted.Body.String(), view.Body.String()}
	}

	toAlias, aliasBodies := rename(wrenLegalAlias)
	toFresh, freshBodies := rename("Fresh Placeholder Name")
	requireFlagStored(t, w, toAlias, "hidden_data_conflict")
	for i := range aliasBodies {
		assert.Equal(t, maskedBody(t, freshBodies[i], "Fresh Placeholder Name"), maskedBody(t, aliasBodies[i],
			wrenLegalAlias), "a rename to a hidden alias is told apart from one to a fresh name")
		assert.NotContains(t, aliasBodies[i], "hidden_data_conflict")
		assert.NotContains(t, freshBodies[i], "hidden_data_conflict")
	}
	return toAlias, toFresh
}

// maskedBody is the raw JSON body with the typed name, and the values of every ID and time field, replaced by
// fixed strings, re-encoded.
func maskedBody(t *testing.T, body, typed string) string {
	t.Helper()

	quoted, err := json.Marshal(typed)
	require.NoError(t, err)
	var decoded any
	require.NoError(t, json.Unmarshal([]byte(strings.ReplaceAll(body, string(quoted), `"<typed name>"`)), &decoded))
	return encodeJSON(t, masked(decoded))
}

// maskedFields are the fields whose values differ between two otherwise identical revisions.
var maskedFields = map[string]bool{"id": true, "created_at": true, "submitted_at": true, "updated_at": true,
	"reviewed_at": true}

// masked returns v with the value of every maskedFields field replaced, at any depth.
func masked(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, field := range v {
			if maskedFields[k] && field != nil {
				out[k] = "<masked>"
			} else {
				out[k] = masked(field)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = masked(item)
		}
		return out
	default:
		return v
	}
}

// provenance is the D6 provenance row: A types into a draft of wren a public alias equal to a hidden alias and the
// hidden link in another spelling, and receives a link that is hidden after the draft is saved. The draft's
// preview shows what A typed and never the received link, and so do the submit's response here and, in the
// matrix's rows, the pending version, the author view and the editable document. It returns the submitted
// revision's ID.
func (w revisionLeakWorld) provenance(t *testing.T, fx revisionLeakFixture) int64 {
	t.Helper()
	a := w.viewers[viewerAuthor]

	start := sendOK[jsonObject](t, w.e, http.MethodGet, editablePath("persons", fx.wren.Slug), a, "", http.StatusOK)
	document := editedDocument(t, start, nil)
	aliases, ok := document["aliases"].([]any)
	require.True(t, ok, "the document has aliases")
	links, ok := document["links"].([]any)
	require.True(t, ok, "the document has links")
	require.Contains(t, encodeJSON(t, links), hiddenLaterURL, "the link to hide later is not received")
	document["aliases"] = append(append([]any{}, aliases...),
		jsonObject{nameField: wrenLegalAlias, "kind": domain.PersonAliasKindStage, "is_public": true})
	document["links"] = append(append([]any{}, links...), jsonObject{"url": typedLinkURL, "title": nil,
		"description": nil, "platform": nil, "place_type": nil, collectionIDParam: nil, "locator": nil,
		"roles": []any{domain.LinkRoleOther}, "performances": []any{}})
	draft := saveDraft(t, w.e, a, draftRequest(t, domain.EntityTypePerson, entitySlug(fx.wren.Slug),
		start["base_revision_id"], document, "Add the stage name and a link."))
	draftID := objectID(t, draft, "id")

	catalogtest.New(t, w.db).SetLinkState(hiddenLaterURL, catalogtest.LinkState{IsHidden: true})
	// The draft's document is the editor document as A saved it (D7); its preview and references are the views
	// built from it, which redact what A received by the current state.
	saved := sendOK[jsonObject](t, w.e, http.MethodGet, idPath("/v1/drafts/", draftID), a, "", http.StatusOK)
	views := encodeJSON(t, jsonObject{"preview": saved["preview"], "references": saved["references"]})
	assertNoLeakIn(t, views, typedByAuthor)
	requireTypedOnce(t, views)

	revision := submitDraft(t, w.e, a, draftID)
	require.Equal(t, string(domain.RevisionStatusPending), revision["status"])
	id := objectID(t, revision, "id")
	requireFlagStored(t, w, id, "hidden_alias_collision")
	assertNoLeakIn(t, encodeJSON(t, revision), typedByAuthor)
	return id
}

// typedByAuthor are the marked strings A typed into the provenance revision of wren.
var typedByAuthor = []string{wrenLegalAlias, typedLinkURL}

// provenanceProblems returns what is wrong with an author view of the provenance revision: a typed value missing
// (which would be an oracle), or the hidden alias shown other than once, as the alias A typed.
func provenanceProblems(body string) []string {
	problems := unusedReveals(body, typedByAuthor)
	if n := strings.Count(body, `"`+wrenLegalAlias+`"`); n != 1 {
		problems = append(problems, fmt.Sprintf("the typed alias is shown %d times, not once", n))
	}
	return problems
}

// requireTypedOnce is the check of every author view of the provenance revision.
func requireTypedOnce(t *testing.T, body string) {
	t.Helper()
	assert.Empty(t, provenanceProblems(body), body)
}

// assertNoLeakIn checks that body shows every revealed string, no other marked string and no flag code.
func assertNoLeakIn(t *testing.T, body string, reveals []string) {
	t.Helper()

	assert.Empty(t, unusedReveals(body, reveals), "the response does not show these revealed strings")
	assert.Empty(t, leakedMarker(body, reveals), "the response shows a marked string:\n%s", body)
	for _, code := range flagCodes {
		assert.NotContains(t, body, code, "the response names a moderator flag")
	}
}

// assertRow sends req and checks its response; it then checks that the blocked user's credentials get only the
// user_blocked error for the same request.
func (w revisionLeakWorld) assertRow(t *testing.T, req revisionLeakRequest) {
	t.Helper()

	rec := w.e.send(t, http.MethodGet, req.path, w.viewers[req.viewer], "")
	require.Equal(t, req.status, rec.Code, rec.Body.String())
	body := rec.Body.String()
	switch {
	case req.full:
	case req.status == http.StatusOK:
		assertNoLeakIn(t, body, req.reveals)
	default:
		requireErrorBodyOnly(t, body, httpjson.CodeNotFound)
	}
	if req.check != nil {
		req.check(t, body)
	}

	for _, blocked := range w.blocked {
		rec := w.e.send(t, http.MethodGet, req.path, blocked, "")
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		requireErrorBodyOnly(t, rec.Body.String(), httpjson.CodeUserBlocked)
	}
}

// requireErrorBodyOnly checks that body is an Error with code and nothing else, and holds no marked string.
func requireErrorBodyOnly(t *testing.T, body string, code httpjson.ErrorCode) {
	t.Helper()

	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &decoded))
	assert.Equal(t, string(code), decoded["code"])
	assert.ElementsMatch(t, []string{"code", "message"}, mapKeys(decoded))
	assert.Empty(t, leakedMarker(body, nil), "the error shows a marked string")
}

// mapKeys returns the keys of m.
func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// newTermDocument is the editor document of a new topic named name.
func newTermDocument(name string) jsonObject {
	return jsonObject{"facet": domain.FacetTopic, "name": name, "display_name": name, "parent": nil,
		"related_affiliation": nil, "description_md": nil, "position": 0, "aliases": []any{}}
}

// revisionLeakMatrix has, for each authenticated read in the spec, the requests to send it as each viewer.
// TestHTTP_RevisionLeakMatrix_EveryAuthenticatedReadHasARow fails for an authenticated GET without a row here or in
// revisionLeakExempt.
var revisionLeakMatrix = map[openapitest.Operation]func(fx revisionLeakFixture) []revisionLeakRequest{
	{Method: http.MethodGet, Path: "/v1/persons/{slug}/editable"}: personEditableRows,
	{Method: http.MethodGet, Path: "/v1/pieces/{slug}/editable"}: func(fx revisionLeakFixture) []revisionLeakRequest {
		// The references label no withheld entity: neither the no-page person nor another user's pending term.
		withheld := func(t *testing.T, body string) {
			t.Helper()
			for _, s := range []string{noPageName, fx.noPage.Slug, suggestedTermName, hiddenLinkURL} {
				assert.NotContains(t, body, s)
			}
		}
		path := editablePath("pieces", fx.piece.Slug)
		return []revisionLeakRequest{
			{viewer: viewerAuthor, path: path, status: http.StatusOK, reveals: []string{pendingNotes}, check: withheld},
			{viewer: viewerOther, path: path, status: http.StatusOK, check: withheld},
		}
	},
	{Method: http.MethodGet, Path: "/v1/terms/{slug}/editable"}: func(fx revisionLeakFixture) []revisionLeakRequest {
		return []revisionLeakRequest{
			{viewer: viewerAuthor, path: editablePath("terms", fx.westmarch.Slug), status: http.StatusOK},
			{viewer: viewerOther, path: editablePath("terms", fx.westmarch.Slug), status: http.StatusOK},
			{viewer: viewerAuthor, path: editablePath("terms", fx.pendingTermSlug), status: http.StatusOK,
				reveals: []string{pendingTerm}},
			{viewer: viewerOther, path: editablePath("terms", fx.pendingTermSlug), status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/events/{slug}/editable"}: func(fx revisionLeakFixture) []revisionLeakRequest {
		return []revisionLeakRequest{
			{viewer: viewerAuthor, path: editablePath("events", fx.moot.Slug), status: http.StatusOK,
				reveals: []string{pendingEvent}},
			{viewer: viewerOther, path: editablePath("events", fx.moot.Slug), status: http.StatusOK},
		}
	},
	{Method: http.MethodGet, Path: "/v1/collections/{collection_id}/editable"}: func(
		fx revisionLeakFixture,
	) []revisionLeakRequest {
		// The unapproved region is withheld.
		path := editablePath("collections", idPath("", fx.archive.ID))
		return []revisionLeakRequest{
			{viewer: viewerAuthor, path: path, status: http.StatusOK},
			{viewer: viewerOther, path: path, status: http.StatusOK},
		}
	},
	{Method: http.MethodGet, Path: "/v1/pieces/{slug}/pending"}: func(fx revisionLeakFixture) []revisionLeakRequest {
		return authorOnly(pendingPath("pieces", fx.piece.Slug), pendingNotes, pendingSummary)
	},
	{Method: http.MethodGet, Path: "/v1/persons/{slug}/pending"}: func(fx revisionLeakFixture) []revisionLeakRequest {
		rows := authorOnly(pendingPath("persons", fx.wren.Slug), typedByAuthor...)
		rows[0].check = requireTypedOnce
		return rows
	},
	{Method: http.MethodGet, Path: "/v1/terms/{slug}/pending"}: func(fx revisionLeakFixture) []revisionLeakRequest {
		return append(authorOnly(pendingPath("terms", fx.pendingTermSlug), pendingTerm),
			authorOnly(pendingPath("terms", fx.westmarch.Slug), flaggedSummary)...)
	},
	{Method: http.MethodGet, Path: "/v1/events/{slug}/pending"}: func(fx revisionLeakFixture) []revisionLeakRequest {
		return authorOnly(pendingPath("events", fx.moot.Slug), pendingEvent)
	},
	{Method: http.MethodGet, Path: "/v1/collections/{collection_id}/pending"}: func(
		fx revisionLeakFixture,
	) []revisionLeakRequest {
		path := pendingPath("collections", idPath("", fx.archive.ID))
		return []revisionLeakRequest{
			{viewer: viewerAuthor, path: path, status: http.StatusNotFound},
			{viewer: viewerOther, path: path, status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: "/v1/drafts/{draft_id}"}: func(fx revisionLeakFixture) []revisionLeakRequest {
		// Drafts are private, from moderators too.
		path := idPath("/v1/drafts/", fx.pieceDraft)
		return []revisionLeakRequest{
			{viewer: viewerAuthor, path: path, status: http.StatusOK, reveals: []string{draftTitle}},
			{viewer: viewerOther, path: path, status: http.StatusNotFound},
			{viewer: viewerModerator, path: path, status: http.StatusNotFound},
		}
	},
	{Method: http.MethodGet, Path: myRevisionsPath}: func(fx revisionLeakFixture) []revisionLeakRequest {
		// The list shows titles and summaries.
		return []revisionLeakRequest{
			{viewer: viewerAuthor, path: myRevisionsPath, status: http.StatusOK,
				reveals: []string{pendingSummary, flaggedSummary, pendingTerm, draftTitle, wrenLegalAlias}},
			{viewer: viewerOther, path: myRevisionsPath, status: http.StatusOK},
		}
	},
	{Method: http.MethodGet, Path: myRevisionPath}: func(fx revisionLeakFixture) []revisionLeakRequest {
		var rows []revisionLeakRequest
		for _, own := range fx.own {
			rows = append(rows, authorOnly(idPath("/v1/me/revisions/", own.id), own.reveals...)...)
		}
		return rows
	},
}

// authorOnly are the rows of a read of A's own revision at path: A sees reveals, B gets 404.
func authorOnly(path string, reveals ...string) []revisionLeakRequest {
	return []revisionLeakRequest{
		{viewer: viewerAuthor, path: path, status: http.StatusOK, reveals: reveals},
		{viewer: viewerOther, path: path, status: http.StatusNotFound},
	}
}

// personEditableRows are the rows of the persons' editable documents: wren's withholds the hidden aliases and links
// from contributors and moderator tokens, and shows them to a moderator; the no-page person's shows the identity.
func personEditableRows(fx revisionLeakFixture) []revisionLeakRequest {
	wren := editablePath("persons", fx.wren.Slug)
	noPage := editablePath("persons", fx.noPage.Slug)
	identity := []string{fx.noPage.Slug, noPageName}
	return []revisionLeakRequest{
		// A starts from their pending provenance revision.
		{viewer: viewerAuthor, path: wren, status: http.StatusOK, reveals: typedByAuthor, check: requireTypedOnce},
		{viewer: viewerOther, path: wren, status: http.StatusOK},
		{viewer: viewerModerator, path: wren, status: http.StatusOK, full: true, check: moderatorSeesAll},
		{viewer: viewerModeratorToken, path: wren, status: http.StatusOK, check: noLinkStates},
		{viewer: viewerAuthor, path: noPage, status: http.StatusOK, reveals: identity},
		{viewer: viewerOther, path: noPage, status: http.StatusOK, reveals: identity},
	}
}

// moderatorSeesAll checks that a moderator's editable document of wren is complete: the hidden aliases and links,
// with their link states.
func moderatorSeesAll(t *testing.T, body string) {
	t.Helper()

	for _, hidden := range []string{wrenLegalAlias, wrenSecondAlias, hiddenLinkURL, hiddenLaterURL} {
		assert.Contains(t, body, `"`+hidden+`"`)
	}
	var editable struct {
		LinkStates []struct {
			URL      string `json:"url"`
			IsHidden bool   `json:"is_hidden"`
		} `json:"link_states"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &editable))
	hidden := map[string]bool{}
	for _, s := range editable.LinkStates {
		hidden[s.URL] = s.IsHidden
	}
	assert.Equal(t, map[string]bool{"https://example.org/wren": false, hiddenLinkURL: true, hiddenLaterURL: true},
		hidden)
}

// noLinkStates checks that an editable document has an empty link_states, as every contributor's has.
func noLinkStates(t *testing.T, body string) {
	t.Helper()

	var editable struct {
		LinkStates []any `json:"link_states"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &editable))
	assert.NotNil(t, editable.LinkStates)
	assert.Empty(t, editable.LinkStates)
}

// revisionLeakExempt are the authenticated reads with no row, and why.
var revisionLeakExempt = map[openapitest.Operation]string{
	{Method: http.MethodGet, Path: mePath}:     "the caller's own account, which holds no catalogue data",
	{Method: http.MethodGet, Path: tokensPath}: "the caller's API tokens, which hold no catalogue data",
}

// TestHTTP_RevisionLeakMatrix sends every revisionLeakMatrix request to the production handler over a private
// database holding the revision leak fixture, as contributors A (the author) and B, a moderator in an Auth0
// session and the same moderator with an API token, and as a blocked user, who gets only 403 user_blocked. No
// response shows a marked string it does not reveal, and none of the records the handler logged holds one.
func TestHTTP_RevisionLeakMatrix(t *testing.T) {
	t.Parallel()

	w, fx := newRevisionLeakWorld(t)
	for op, rows := range revisionLeakMatrix {
		t.Run(op.Method+" "+op.Path, func(t *testing.T) {
			for _, req := range rows(fx) {
				t.Run(string(req.viewer)+" "+req.path, func(t *testing.T) {
					w.assertRow(t, req)
				})
			}
		})
	}

	// The blocked user cannot write either.
	for _, blocked := range w.blocked {
		sendRejected(t, w.e, http.MethodPost, draftsPath, blocked, draftRequest(t, domain.EntityTypePiece, nil, nil,
			newPieceDocument("Blocked"), "A new piece."), http.StatusForbidden, httpjson.CodeUserBlocked)
		sendRejected(t, w.e, http.MethodPost, revisionsPath, blocked, submitBody(fx.pieceDraft),
			http.StatusForbidden, httpjson.CodeUserBlocked)
	}

	w.logs.requireNoMarkedLogs(t)
}

// TestHTTP_RevisionLeakMatrix_PlantedLeakFails shows that the matrix's checks can fail: a hidden alias planted as
// public in a copy of an editable document is reported by the scanner, and the alias A typed removed from a copy of
// their pending version fails the provenance row's check that typed values are present.
func TestHTTP_RevisionLeakMatrix_PlantedLeakFails(t *testing.T) {
	t.Parallel()

	w, fx := newRevisionLeakWorld(t)

	rec := w.e.send(t, http.MethodGet, editablePath("persons", fx.wren.Slug), w.viewers[viewerOther], "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	editable := rec.Body.String()
	require.Empty(t, leakedMarker(editable, nil))
	require.Contains(t, editable, `"aliases":[`, "the document has no aliases, so this test proves nothing")
	planted := strings.Replace(editable, `"aliases":[`,
		`"aliases":[{"name":"`+wrenLegalAlias+`","kind":"legal","is_public":true},`, 1)
	assert.NotEmpty(t, leakedMarker(planted, nil))

	rec = w.e.send(t, http.MethodGet, pendingPath("persons", fx.wren.Slug), w.viewers[viewerAuthor], "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	pending := rec.Body.String()
	require.Empty(t, provenanceProblems(pending))
	assert.NotEmpty(t, provenanceProblems(strings.ReplaceAll(pending, `"`+wrenLegalAlias+`"`, `"Wren"`)))
}

// TestHTTP_RevisionLeakMatrix_EveryAuthenticatedReadHasARow fails when an authenticated GET in the spec has
// neither a row in revisionLeakMatrix nor an exemption in revisionLeakExempt.
func TestHTTP_RevisionLeakMatrix_EveryAuthenticatedReadHasARow(t *testing.T) {
	t.Parallel()

	contract := openapitest.New(t)
	operations := contract.Operations()
	authenticatedRead := func(op openapitest.Operation) bool {
		return op.Method == http.MethodGet && strings.HasPrefix(op.Path, "/v1/") && !contract.Public(op)
	}
	for op := range revisionLeakMatrix {
		assert.Contains(t, operations, op)
		assert.True(t, authenticatedRead(op), "%s %s has a row but is not an authenticated read in the spec", op.Method, op.Path)
	}
	for op := range revisionLeakExempt {
		assert.Contains(t, operations, op)
		assert.True(t, authenticatedRead(op), "%s %s is exempt but is not an authenticated read in the spec", op.Method, op.Path)
	}
	for _, op := range operations {
		_, hasRow := revisionLeakMatrix[op]
		_, exempt := revisionLeakExempt[op]
		if authenticatedRead(op) {
			assert.NotEqual(t, hasRow, exempt, "%s %s needs exactly one of a row and an exemption", op.Method, op.Path)
		}
	}
}
