//go:build integration

package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/domaintest"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
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
	// viewerAuthorToken is A with an API token.
	viewerAuthorToken revisionViewer = "AT"
	// viewerModeratorToken is the same moderator with an API token, a contributor.
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
	pendingArchive = "leakmark-pending-archive"
	// rejectedWrenNotes are the notes of A's edit of wren that the moderator rejects with rejectionNote.
	rejectedWrenNotes = "leakmark-rejected-wren-notes"
	// hiddenSourceNotes are the notes of wren's and the piece's preferences whose source is the hidden link (as
	// respelledHiddenLinkURL for wren), which only an interactive moderator's editable document shows.
	hiddenSourceNotes = "leakmark-hidden-source-preference"
	// respelledHiddenLinkURL normalizes to hiddenLinkURL.
	respelledHiddenLinkURL = "HTTPS://EXAMPLE.org/leakmark-hidden-link?utm_source=x"
	// wrenPreferencesURL is the source of wren's preference everyone may see.
	wrenPreferencesURL = "https://example.org/wren-preferences"
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
	// renameToAlias is A's (withdrawn) rename of wren to a hidden alias, flagged hidden_data_conflict.
	renameToAlias int64
	// rejected is A's edit of wren that the moderator rejected: A typed nothing hidden, but received wren's
	// hidden aliases and hidden link, which the stored snapshot holds.
	rejected int64
	// own are A's revisions, drafts included, with the marked strings of A's own each shows A.
	own []ownRevision
}

// ownRevision is one of A's revisions and the marked strings A typed that its author view shows.
type ownRevision struct {
	id      int64
	reveals []string
	// check, if not nil, makes further assertions on the author's view.
	check func(t *testing.T, body string)
}

// revisionLeakRequest is a GET the revision leak matrix sends as viewer, and its expected status.
type revisionLeakRequest struct {
	viewer revisionViewer
	path   string
	status int
	// code is the error code of a response that is not 200; not_found if empty.
	code httpjson.ErrorCode
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

// newLogCapture returns a capture and a logger that writes every record to it, DEBUG included.
func newLogCapture() (*logCapture, *slog.Logger) {
	logs := &logCapture{}
	return logs, slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// requireNoMarkedLogs checks that records were captured and that none of them holds a marked string.
func (l *logCapture) requireNoMarkedLogs(t *testing.T) {
	t.Helper()

	l.requireNoneOf(t, []string{leakMarker})
}

// requireNoneOf checks that records were captured and that none of them holds any of private, compared
// case-insensitively.
func (l *logCapture) requireNoneOf(t *testing.T, private []string) {
	t.Helper()

	logs := l.String()
	require.Contains(t, logs, "request completed", "no request was logged, so this check proves nothing")
	for line := range strings.Lines(logs) {
		for _, p := range private {
			assert.NotContains(t, strings.ToLower(line), strings.ToLower(p), "a log record holds private data")
		}
	}
}

// newRevisionLeakWorld seeds a private database with the revision leak fixture: catalogue rows through
// catalogtest, and every contributor's revision through the HTTP API, as its author would write it.
func newRevisionLeakWorld(t *testing.T) (revisionLeakWorld, revisionLeakFixture) {
	t.Helper()

	logs, logger := newLogCapture()
	w := revisionLeakWorld{
		revisionWorld: newRevisionWorld(t, logger, revisionLeakModeratorID),
		logs:          logs,
	}
	w.viewers = map[revisionViewer]string{
		viewerAuthor: w.auth0(t, revisionLeakAuthorID), viewerOther: w.auth0(t, revisionLeakOtherID),
		viewerModerator: w.auth0(t, revisionLeakModeratorID),
	}
	for _, authorization := range w.viewers {
		sendOK[jsonObject](t, w.e, http.MethodGet, mePath, authorization, "", http.StatusOK)
	}
	w.viewers[viewerAuthorToken] = w.apiToken(t, w.viewers[viewerAuthor])
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
		Aliases: []domain.DocPersonAlias{{Name: noPagePublicAlias, Kind: domain.PersonAliasKindStage,
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
		Preferences: []domain.DocPreference{
			hiddenSourcePreference(domain.PreferenceDimensionPerformancePermission, respelledHiddenLinkURL,
				domain.PreferenceChoiceGiven),
			domaintest.Preference(domain.PreferenceDimensionRecording, wrenPreferencesURL,
				domain.PreferenceChoiceAskBeforeRecording),
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
		Preferences: []domain.DocPreference{
			hiddenSourcePreference(domain.PreferenceDimensionAttribution, hiddenLinkURL, domain.PreferenceChoiceSCAName),
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

// hiddenSourcePreference is a preference whose source is url, a spelling of the hidden link, with hiddenSourceNotes.
func hiddenSourcePreference(dimension domain.PreferenceDimension, url string,
	choice domain.PreferenceChoice,
) domain.DocPreference {
	p := domaintest.Preference(dimension, url, choice)
	p.Value.Notes = new(hiddenSourceNotes)

	return p
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

	archive := w.submitEdit(t, "collections", idPath("", fx.archive.ID), domain.EntityTypeCollection,
		jsonObject{descriptionField: pendingArchive}, "Describe the archive.")

	renameToAlias, renameToFresh := w.validationOracle(t, fx)
	w.duplicateOracle(t)
	fx.renameToAlias = renameToAlias
	fx.rejected = w.rejectedEdit(t, fx)
	provenance := w.provenance(t, fx)

	// A reports a visible link of the piece, with a message for the moderators' inbox.
	sendOK[jsonObject](t, w.e, http.MethodPost, reportLinkPath, a,
		reportLinkRequest(t, "https://example.org/lantern", reportMessage), http.StatusCreated)
	// Someone without an account writes to the moderators; their personal data is for the inbox alone.
	sendContact(t, w.e, contactBody("opt_out", contactName, contactEmail, contactMessage))

	fx.own = []ownRevision{
		{id: piece, reveals: []string{pendingNotes, pendingSummary}},
		{id: event, reveals: []string{pendingEvent}},
		{id: archive, reveals: []string{pendingArchive}},
		{id: fx.rejected, reveals: []string{rejectedWrenNotes}, check: rejectedAuthorView},
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

	entity := entitySlug(key)
	if entityType == domain.EntityTypeCollection {
		id, err := strconv.ParseInt(key, 10, 64)
		require.NoError(t, err)
		entity = jsonObject{"slug": nil, collectionIDParam: id}
	}
	start := sendOK[jsonObject](t, w.e, http.MethodGet, editablePath(collection, key), a, "", http.StatusOK)
	draft := saveDraft(t, w.e, a, draftRequest(t, entityType, entity, start["base_revision_id"],
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

// validationOracle is the validation-oracle row: A renames wren to its hidden alias, and (after withdrawing that) to a fresh
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

// provenance covers A typing into a draft of wren a public alias equal to a hidden alias and the
// hidden link in another spelling, and receiving a link that is hidden after the draft is saved. The draft's
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
	// The draft's document is the editor document as A saved it; its preview and references are the views
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
	case req.code != "":
		requireErrorBodyOnly(t, body, req.code)
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
			{viewer: viewerModerator, path: path, status: http.StatusOK, full: true, check: hiddenSourceShown},
			{viewer: viewerModeratorToken, path: path, status: http.StatusOK, check: withheld},
		}
	},
	{Method: http.MethodGet, Path: "/v1/terms/{slug}/editable"}: func(fx revisionLeakFixture) []revisionLeakRequest {
		return []revisionLeakRequest{
			{viewer: viewerAuthor, path: editablePath("terms", fx.westmarch.Slug), status: http.StatusOK},
			{viewer: viewerOther, path: editablePath("terms", fx.westmarch.Slug), status: http.StatusOK},
			{viewer: viewerAuthor, path: editablePath("terms", fx.pendingTermSlug), status: http.StatusOK,
				reveals: []string{pendingTerm}},
			{viewer: viewerOther, path: editablePath("terms", fx.pendingTermSlug), status: http.StatusNotFound},
			{viewer: viewerModeratorToken, path: editablePath("terms", fx.pendingTermSlug), status: http.StatusNotFound},
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
		// The unapproved region is withheld; A starts from their pending edit, which received it.
		path := editablePath("collections", idPath("", fx.archive.ID))
		return []revisionLeakRequest{
			{viewer: viewerAuthor, path: path, status: http.StatusOK, reveals: []string{pendingArchive}},
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
		// A's pending edit received the unapproved region, which their view withholds.
		return authorOnly(pendingPath("collections", idPath("", fx.archive.ID)), pendingArchive)
	},
	{Method: http.MethodGet, Path: "/v1/drafts/{draft_id}"}: func(fx revisionLeakFixture) []revisionLeakRequest {
		// Drafts are private, from moderators too.
		path := idPath("/v1/drafts/", fx.pieceDraft)
		return []revisionLeakRequest{
			{viewer: viewerAuthor, path: path, status: http.StatusOK, reveals: []string{draftTitle}},
			{viewer: viewerOther, path: path, status: http.StatusNotFound},
			{viewer: viewerModerator, path: path, status: http.StatusNotFound},
			{viewer: viewerModeratorToken, path: path, status: http.StatusNotFound},
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
			some := authorOnly(idPath("/v1/me/revisions/", own.id), own.reveals...)
			some[0].check = own.check
			rows = append(rows, some...)
		}
		return rows
	},
	{Method: http.MethodGet, Path: moderationQueuePath}: func(revisionLeakFixture) []revisionLeakRequest {
		return moderatorOnly(moderationQueuePath, func(t *testing.T, body string) {
			t.Helper()
			assert.Contains(t, body, `"`+pendingSummary+`"`, "the queue does not list A's pending revision")
		})
	},
	{Method: http.MethodGet, Path: moderationRevisionRoute}: func(
		fx revisionLeakFixture,
	) []revisionLeakRequest {
		// The validation oracle's rename to a hidden alias: A never sees hidden_data_conflict, the moderator does.
		return moderatorOnly(moderationPath(fx.renameToAlias), func(t *testing.T, body string) {
			t.Helper()
			assert.Contains(t, flagCodesOf(t, body), "hidden_data_conflict")
		})
	},
	{Method: http.MethodGet, Path: moderationReportsPath}: func(revisionLeakFixture) []revisionLeakRequest {
		return moderatorOnly(moderationReportsPath, func(t *testing.T, body string) {
			t.Helper()
			assert.Contains(t, body, `"`+reportMessage+`"`, "the inbox does not show A's report message")
		})
	},
	{Method: http.MethodGet, Path: contactRequestsPath}: func(revisionLeakFixture) []revisionLeakRequest {
		return moderatorOnly(contactRequestsPath, func(t *testing.T, body string) {
			t.Helper()
			for _, pii := range []string{contactName, contactEmail, contactMessage} {
				assert.Contains(t, body, `"`+pii+`"`, "the inbox does not show the contact request's data")
			}
		})
	},
	{Method: http.MethodGet, Path: moderationLinksPath}: func(revisionLeakFixture) []revisionLeakRequest {
		// The hidden link, in full, as the moderator sees it.
		return moderatorOnly(moderationLinksPath+"?url="+url.QueryEscape(hiddenLinkURL), func(t *testing.T, body string) {
			t.Helper()
			var link struct {
				URL      string `json:"url"`
				IsHidden bool   `json:"is_hidden"`
			}
			require.NoError(t, json.Unmarshal([]byte(body), &link))
			assert.Equal(t, hiddenLinkURL, link.URL)
			assert.True(t, link.IsHidden)
		})
	},
}

// moderatorOnly are the rows of a moderator view at path: contributors A and B, and A's API token, get 403
// moderator_required, the moderator's API token 403 api_token_not_allowed, each with the Error body only, and the
// moderator in an Auth0 session the full view, which check asserts on.
func moderatorOnly(path string, check func(t *testing.T, body string)) []revisionLeakRequest {
	return []revisionLeakRequest{
		{viewer: viewerAuthor, path: path, status: http.StatusForbidden, code: httpjson.CodeModeratorRequired},
		{viewer: viewerOther, path: path, status: http.StatusForbidden, code: httpjson.CodeModeratorRequired},
		{viewer: viewerModeratorToken, path: path, status: http.StatusForbidden, code: httpjson.CodeAPIAuthNotAllowed},
		{viewer: viewerAuthorToken, path: path, status: http.StatusForbidden, code: httpjson.CodeModeratorRequired},
		{viewer: viewerModerator, path: path, status: http.StatusOK, full: true, check: check},
	}
}

// flagCodesOf returns the codes of the flags of a moderation revision view, in order.
func flagCodesOf(t *testing.T, body string) []string {
	t.Helper()

	var revision struct {
		Flags []struct {
			Code string `json:"code"`
		} `json:"flags"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &revision))
	codes := make([]string, 0, len(revision.Flags))
	for _, flag := range revision.Flags {
		codes = append(codes, flag.Code)
	}
	return codes
}

// The paths of link reports and of moderator views without a constant elsewhere.
const (
	reportLinkPath          = "/v1/link-reports"
	moderationRevisionRoute = "/v1/moderation/revisions/{revision_id}"
	moderationLinksPath     = "/v1/moderation/links"
)

// reportLinkRequest is the body of a report of the link at linkURL with message.
func reportLinkRequest(t *testing.T, linkURL, message string) string {
	t.Helper()

	return encodeJSON(t, controller.LinkReportCreateRequest{URL: linkURL, Reason: domain.LinkReportReasonOther,
		Message: &message})
}

// assertHiddenLinkReportIsUnknown checks that A's report of the hidden link gets the same 404, byte for byte, as a
// report of a URL no link has, so that a report cannot tell a hidden link from no link; and that the blocked user
// cannot report links.
func (w revisionLeakWorld) assertHiddenLinkReportIsUnknown(t *testing.T) {
	t.Helper()
	a := w.viewers[viewerAuthor]

	hidden := w.e.send(t, http.MethodPost, reportLinkPath, a, reportLinkRequest(t, hiddenLinkURL, "Broken."))
	require.Equal(t, http.StatusNotFound, hidden.Code, hidden.Body.String())
	requireErrorBodyOnly(t, hidden.Body.String(), httpjson.CodeNotFound)
	unknown := w.e.send(t, http.MethodPost, reportLinkPath, a,
		reportLinkRequest(t, "https://example.org/no-such-link", "Broken."))
	require.Equal(t, http.StatusNotFound, unknown.Code, unknown.Body.String())
	assert.Equal(t, unknown.Body.String(), hidden.Body.String(), "a hidden link's report is told apart from no link's")

	for _, blocked := range w.blocked {
		sendRejected(t, w.e, http.MethodPost, reportLinkPath, blocked,
			reportLinkRequest(t, "https://example.org/lantern", "Broken."), http.StatusForbidden,
			httpjson.CodeUserBlocked)
	}
}

// authorOnly are the rows of a read of A's own revision at path: A sees reveals (and so does A's API token, as
// withAuthorToken adds), and B and the moderator's API token get 404.
func authorOnly(path string, reveals ...string) []revisionLeakRequest {
	return []revisionLeakRequest{
		{viewer: viewerAuthor, path: path, status: http.StatusOK, reveals: reveals},
		{viewer: viewerOther, path: path, status: http.StatusNotFound},
		{viewer: viewerModeratorToken, path: path, status: http.StatusNotFound},
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
		{viewer: viewerOther, path: wren, status: http.StatusOK, check: wrenPreferenceShown},
		{viewer: viewerModerator, path: wren, status: http.StatusOK, full: true, check: moderatorSeesAll},
		{viewer: viewerModeratorToken, path: wren, status: http.StatusOK, check: noLinkStates},
		{viewer: viewerAuthor, path: noPage, status: http.StatusOK, reveals: identity},
		{viewer: viewerOther, path: noPage, status: http.StatusOK, reveals: identity},
	}
}

// moderatorSeesAll checks that a moderator's editable document of wren is complete: the hidden aliases and links,
// with their link states, and the preference whose source is hidden.
func moderatorSeesAll(t *testing.T, body string) {
	t.Helper()

	for _, hidden := range []string{wrenLegalAlias, wrenSecondAlias, hiddenLinkURL, hiddenLaterURL, hiddenSourceNotes,
		respelledHiddenLinkURL} {
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

// hiddenSourceShown checks that a moderator's editable document shows the preference whose source is hidden.
func hiddenSourceShown(t *testing.T, body string) {
	t.Helper()

	assert.Contains(t, body, `"`+hiddenSourceNotes+`"`)
}

// wrenPreferenceShown checks that a contributor's editable document of wren shows the preference whose source is
// not hidden.
func wrenPreferenceShown(t *testing.T, body string) {
	t.Helper()

	assert.Contains(t, body, `"source_url":"`+wrenPreferencesURL+`"`)
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
	requireRevisionLeakMarkersStored(t, w, fx)
	for op, rows := range revisionLeakMatrix {
		t.Run(op.Method+" "+op.Path, func(t *testing.T) {
			for _, req := range withAuthorToken(rows(fx)) {
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
	w.assertHiddenLinkReportIsUnknown(t)

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

// duplicateOracle is the duplicate check's row: A drafts a new person named as the public alias of the person who
// prefers no page, and as a fresh name. A person who prefers no page is found only by name, so the two drafts, as
// saved and as read back, are byte-equal once the typed name, IDs and times are masked, and show no duplicate;
// a draft named as that person does show it, so that the check is seen to run. The drafts are discarded.
func (w revisionLeakWorld) duplicateOracle(t *testing.T) {
	t.Helper()
	a := w.viewers[viewerAuthor]

	draft := func(name string) []string {
		saved := w.e.send(t, http.MethodPost, draftsPath, a, draftRequest(t, domain.EntityTypePerson, nil, nil,
			newPersonDocument(name), "A new bard."))
		require.Equal(t, http.StatusCreated, saved.Code, saved.Body.String())
		var body jsonObject
		require.NoError(t, json.Unmarshal(saved.Body.Bytes(), &body))
		path := idPath(draftsPath+"/", objectID(t, body, "id"))
		read := w.e.send(t, http.MethodGet, path, a, "")
		require.Equal(t, http.StatusOK, read.Code, read.Body.String())
		discarded := w.e.send(t, http.MethodDelete, path, a, "")
		require.Equal(t, http.StatusNoContent, discarded.Code, discarded.Body.String())

		return []string{saved.Body.String(), read.Body.String()}
	}

	aliasBodies := draft(noPagePublicAlias)
	freshBodies := draft("Fresh Placeholder Bard")
	for i := range aliasBodies {
		assert.Equal(t, maskedBody(t, freshBodies[i], "Fresh Placeholder Bard"), maskedBody(t, aliasBodies[i],
			noPagePublicAlias), "a draft named as an alias of a person who prefers no page is told apart")
	}
	byName := draft(noPageName)
	for _, body := range byName {
		assert.Contains(t, body, `"matched":"name"`, "a draft named as a person who prefers no page shows them")
	}
}

// rejectionNote is the moderator's review note on A's rejected edit of wren; it holds nothing private.
const rejectionNote = "Please cite a source for this."

// rebasedWrenNotes are the notes the moderator gave A's edit of wren in a rebase before rejecting it. A's view
// shows what A submitted, so A never sees them.
const rebasedWrenNotes = "leakmark-rebased-wren-notes"

// rejectedEdit has A edit wren's notes, typing nothing hidden, and the moderator rebase the edit with
// rebasedWrenNotes and then reject it with rejectionNote. It returns the rejected revision's ID, having checked
// that its stored snapshot holds what A must not be shown: the moderator's notes, and what A received, wren's hidden
// aliases and hidden link.
func (w revisionLeakWorld) rejectedEdit(t *testing.T, fx revisionLeakFixture) int64 {
	t.Helper()
	m := w.viewers[viewerModerator]

	id := w.submitEdit(t, "persons", fx.wren.Slug, domain.EntityTypePerson,
		jsonObject{notesField: rejectedWrenNotes}, "Add notes.")
	view := sendOK[jsonObject](t, w.e, http.MethodGet, moderationPath(id), m, "", http.StatusOK)
	rebased := editedDocument(t, view, jsonObject{notesField: rebasedWrenNotes})
	sendOK[jsonObject](t, w.e, http.MethodPost, idPath("/v1/revisions/", id)+"/rebase", m,
		encodeJSON(t, jsonObject{"entity_type": domain.EntityTypePerson,
			"current_revision_id": view["current_revision_id"], "document": rebased}), http.StatusOK)
	view = sendOK[jsonObject](t, w.e, http.MethodGet, moderationPath(id), m, "", http.StatusOK)
	acknowledged := []string{}
	for _, code := range flagCodesOf(t, encodeJSON(t, view)) {
		if !slices.Contains(acknowledged, code) {
			acknowledged = append(acknowledged, code)
		}
	}
	rejected := sendOK[jsonObject](t, w.e, http.MethodPost, idPath("/v1/revisions/", id)+"/reject", m,
		encodeJSON(t, jsonObject{"review_note": rejectionNote, acknowledgedFlagsField: acknowledged}), http.StatusOK)
	require.Equal(t, string(domain.RevisionStatusRejected), rejected["status"])

	var stored string
	require.NoError(t, w.db.QueryRowContext(t.Context(), "SELECT snapshot_json FROM revisions WHERE id = ? "+
		"AND status = 'rejected'", id).Scan(&stored))
	for _, private := range []string{rebasedWrenNotes, wrenLegalAlias, wrenSecondAlias, hiddenLinkURL} {
		require.Contains(t, stored, `"`+private+`"`, "the rejected revision does not hold what A must not see")
	}
	return id
}

// rejectedAuthorView checks that A's view of their rejected revision shows the rejection: its status, review note
// and review time.
func rejectedAuthorView(t *testing.T, body string) {
	t.Helper()

	var revision struct {
		Status     string  `json:"status"`
		ReviewNote *string `json:"review_note"`
		ReviewedAt *string `json:"reviewed_at"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &revision))
	assert.Equal(t, string(domain.RevisionStatusRejected), revision.Status)
	assert.Equal(t, new(rejectionNote), revision.ReviewNote)
	assert.NotNil(t, revision.ReviewedAt)
}

// requireRevisionLeakMarkersStored checks that the fixture stores the marked strings the matrix's rows withhold,
// so that no row passes because its marker went missing: the no-page person's notes, pronouns, public alias and
// credit note, the unapproved region, and A's pending edit of the archive, which received the region.
func requireRevisionLeakMarkersStored(t *testing.T, w revisionLeakWorld, fx revisionLeakFixture) {
	t.Helper()

	for _, check := range []struct {
		query string
		args  []any
	}{
		{"SELECT COUNT(*) FROM persons WHERE id = ? AND status = 'prefers_no_page' AND notes_md = ? AND pronouns = ?",
			[]any{fx.noPage.ID, "Leakmark nopage notes", "leakmark/pronoun"}},
		{"SELECT COUNT(*) FROM person_aliases WHERE person_id = ? AND is_public AND name = ?",
			[]any{fx.noPage.ID, noPagePublicAlias}},
		{"SELECT COUNT(*) FROM piece_credits WHERE piece_id = ? AND person_id = ? AND note = ?",
			[]any{fx.piece.ID, fx.noPage.ID, "Leakmark nopage credit note"}},
		{"SELECT COUNT(*) FROM terms WHERE name = ? AND current_revision_id IS NULL", []any{unapprovedRegionName}},
		{"SELECT COUNT(*) FROM revisions r JOIN collections c ON c.id = r.entity_id JOIN terms region " +
			"ON region.id = c.region_term_id WHERE r.entity_type = 'collection' AND r.entity_id = ? " +
			"AND r.status = 'pending' AND region.name = ? " +
			"AND CAST(r.snapshot_json->>'$.region_term_id' AS UNSIGNED) = region.id",
			[]any{fx.archive.ID, unapprovedRegionName}},
	} {
		var count int
		require.NoError(t, w.db.QueryRowContext(t.Context(), check.query, check.args...).Scan(&count), check.query)
		require.Equal(t, 1, count, "%s %v", check.query, check.args)
	}
}

// withAuthorToken returns rows with each of A's rows that is not an error repeated for A's API token, which sees
// what A sees.
func withAuthorToken(rows []revisionLeakRequest) []revisionLeakRequest {
	all := slices.Clone(rows)
	for _, req := range rows {
		if req.viewer == viewerAuthor && req.code == "" {
			req.viewer = viewerAuthorToken
			all = append(all, req)
		}
	}
	return all
}
