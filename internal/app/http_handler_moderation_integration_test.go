//go:build integration

package app

import (
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/importtest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/controller"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	moderationE2EModeratorID = "facebook|moderation-e2e-moderator"
	moderationE2EAuthorID    = "google-oauth2|moderation-e2e-author"
	moderationE2EAuthorPath  = "google-oauth2%7Cmoderation-e2e-author"
	moderationE2EOtherID     = "google-oauth2|moderation-e2e-other"
	moderationQueuePath      = "/v1/moderation/queue"
	moderationReportsPath    = "/v1/moderation/link-reports"
	// Editor document and request fields the test sets.
	titleField      = "title"
	kindField       = "kind"
	aliasesField    = "aliases"
	linksField      = "links"
	rolesField      = "roles"
	documentField   = "document"
	entityTypeField = "entity_type"
	termsField      = "terms"
	urlField        = "url"
)

// moderationWorld is the production handler over a private database holding the scrubbed import, with the
// credentials of contributors A and B, the moderator in an Auth0 session and the moderator's API token.
type moderationWorld struct {
	revisionWorld
	a, b, moderator, modToken string
	// pieces are imported pieces whose links are all visible; renamed is an imported person with a page, and
	// renamedAlias one of its hidden aliases.
	pieces       []domain.ImportedPiece
	renamed      domain.ImportedPerson
	renamedAlias string
	// hiddenAliases are the names of every hidden alias of the import, which no log record may hold.
	hiddenAliases []string
	// logs captures every record the handler writes, DEBUG included.
	logs *logCapture
}

// newModerationWorld applies the scrubbed import to a fresh database whose moderator is bootstrapped, and picks
// the imported entities the test edits: pieces whose links are all visible, and a person with a page and a
// hidden alias.
func newModerationWorld(t *testing.T) moderationWorld {
	t.Helper()

	logs, logger := newLogCapture()
	w := moderationWorld{revisionWorld: newRevisionWorld(t, logger, moderationE2EModeratorID), logs: logs}
	imp, _ := importtest.Scrubbed(t)
	w.hiddenAliases = hiddenAliases(imp)
	catalogtest.New(t, w.db).Import(imp)
	w.a, w.b = w.auth0(t, moderationE2EAuthorID), w.auth0(t, moderationE2EOtherID)
	w.moderator = w.auth0(t, moderationE2EModeratorID)
	w.modToken = w.apiToken(t, w.moderator)

	w.pieces = piecesWithVisibleLinks(imp)
	require.GreaterOrEqual(t, len(w.pieces), 3, "the scrubbed import has pieces with visible links")
	w.renamed, w.renamedAlias = personWithHiddenAlias(imp)
	require.NotEmpty(t, w.renamedAlias, "the scrubbed import has a person with a page and a hidden alias")
	return w
}

// piecesWithVisibleLinks returns the pieces of imp with links, none of them hidden.
func piecesWithVisibleLinks(imp domain.CatalogueImport) []domain.ImportedPiece {
	hiddenURLs := map[string]bool{}
	for _, l := range imp.Links {
		if l.IsHidden {
			hiddenURLs[l.Link.URL] = true
		}
	}
	var pieces []domain.ImportedPiece
	for _, p := range imp.Pieces {
		if len(p.Doc.Links) > 0 && !slices.ContainsFunc(p.Doc.Links, func(l domain.DocPieceLink) bool {
			return hiddenURLs[l.URL]
		}) {
			pieces = append(pieces, p)
		}
	}
	return pieces
}

// personWithHiddenAlias returns the first person of imp with a page and a hidden alias, and that alias; the alias
// is empty if there is none.
func personWithHiddenAlias(imp domain.CatalogueImport) (domain.ImportedPerson, string) {
	for _, p := range imp.Persons {
		if p.Doc.Status == domain.PersonStatusPrefersNoPage {
			continue
		}
		for _, a := range p.Doc.Aliases {
			if !a.IsPublic {
				return p, a.Name
			}
		}
	}
	return domain.ImportedPerson{}, ""
}

// moderationPath is the moderator's view of the revision id.
func moderationPath(id int64) string {
	return idPath("/v1/moderation/revisions/", id)
}

// view returns the moderator's view of the revision id.
func (w moderationWorld) view(t *testing.T, id int64) jsonObject {
	t.Helper()

	return sendOK[jsonObject](t, w.e, http.MethodGet, moderationPath(id), w.moderator, "", http.StatusOK)
}

// approveBody acknowledges every flag of the moderator's view of id that approving needs acknowledged.
func (w moderationWorld) approveBody(t *testing.T, id int64) string {
	t.Helper()

	acknowledged := []string{}
	for _, code := range flagCodesOf(t, encodeJSON(t, w.view(t, id))) {
		if code != string(domain.FlagReferencesPending) && !slices.Contains(acknowledged, code) {
			acknowledged = append(acknowledged, code)
		}
	}
	return encodeJSON(t, jsonObject{"acknowledged_flags": acknowledged})
}

// approve has the moderator approve the revision id, acknowledging its flags, and returns the approved revision.
func (w moderationWorld) approve(t *testing.T, id int64) jsonObject {
	t.Helper()

	approved := sendOK[jsonObject](t, w.e, http.MethodPost, idPath("/v1/revisions/", id)+"/approve", w.moderator,
		w.approveBody(t, id), http.StatusOK)
	assert.Equal(t, string(domain.RevisionStatusApproved), approved["status"])
	return approved
}

// reject has the moderator reject the revision id, returning the rejected revision.
func (w moderationWorld) reject(t *testing.T, id int64) jsonObject {
	t.Helper()

	rejected := sendOK[jsonObject](t, w.e, http.MethodPost, idPath("/v1/revisions/", id)+"/reject", w.moderator,
		`{"review_note":"Not this time.","acknowledged_flags":[]}`, http.StatusOK)
	assert.Equal(t, string(domain.RevisionStatusRejected), rejected["status"])
	return rejected
}

// editable returns authorization's editable document of the entity collection/slug.
func (w moderationWorld) editable(t *testing.T, authorization, collection, slug string) jsonObject {
	t.Helper()

	return sendOK[jsonObject](t, w.e, http.MethodGet, editablePath(collection, slug), authorization, "",
		http.StatusOK)
}

// submitPieceEdit saves and submits authorization's draft of the piece slug on base with document, returning
// the revision.
func (w moderationWorld) submitPieceEdit(t *testing.T, authorization, slug string, base any, document jsonObject,
	summary string,
) jsonObject {
	t.Helper()

	draft := saveDraft(t, w.e, authorization, draftRequest(t, domain.EntityTypePiece, entitySlug(slug), base, document,
		summary))
	return submitDraft(t, w.e, authorization, objectID(t, draft, "id"))
}

// submitNew saves and submits authorization's draft of a new entity with document, returning the pending revision.
func (w moderationWorld) submitNew(t *testing.T, authorization string, entityType domain.EntityType,
	document jsonObject, summary string,
) jsonObject {
	t.Helper()

	draft := saveDraft(t, w.e, authorization, draftRequest(t, entityType, nil, nil, document, summary))
	revision := submitDraft(t, w.e, authorization, objectID(t, draft, "id"))
	require.Equal(t, string(domain.RevisionStatusPending), revision["status"])
	return revision
}

// publicPiece returns the public page of the piece slug.
func (w moderationWorld) publicPiece(t *testing.T, slug string) string {
	t.Helper()

	rec := w.e.send(t, http.MethodGet, "/v1/pieces/"+slug, "", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	return rec.Body.String()
}

// entityOf returns the entity reference of a revision o.
func entityOf(t *testing.T, o jsonObject) jsonObject {
	t.Helper()

	entity, ok := o["entity"].(jsonObject)
	require.True(t, ok, "the revision has no entity: %v", o["entity"])
	return entity
}

// documentLinks returns the links of the editor document document.
func documentLinks(t *testing.T, document jsonObject) []any {
	t.Helper()

	links, ok := document[linksField].([]any)
	require.True(t, ok, "the document has no links")
	return links
}

// newPersonDocument is the editor document of a new community bard named name.
func newPersonDocument(name string) jsonObject {
	return jsonObject{
		nameField: name, kindField: domain.PersonKindCommunityBard, "status": domain.PersonStatusActive,
		"status_date": nil, "status_source_url": nil, "pronouns": nil, notesField: nil,
		"licensing_status": domain.LicensingStatusUnknown, "licensing_notes_md": nil, "is_stub": false,
		aliasesField: []any{}, rolesField: []any{}, "affiliations": []any{}, "relations": []any{}, linksField: []any{},
	}
}

// originTermNotOn returns the slug of a public term of the origin facet, whose terms may be scoped to a part, that
// the editor document is not tagged with.
func originTermNotOn(t *testing.T, w moderationWorld, document jsonObject) string {
	t.Helper()

	tagged := encodeJSON(t, document[termsField])
	terms := sendOK[jsonObject](t, w.e, http.MethodGet, "/v1/terms?facet="+string(domain.FacetOrigin), "", "",
		http.StatusOK)
	list, ok := terms["data"].([]any)
	require.True(t, ok, "the term list has data")
	for _, item := range list {
		slug, ok := item.(jsonObject)["slug"].(string)
		require.True(t, ok, "a term has a slug")
		if !strings.Contains(tagged, `"term":"`+slug+`"`) {
			return slug
		}
	}
	require.FailNow(t, "the scrubbed import has an origin term the piece is not tagged with")
	return ""
}

// TestHTTP_ModerationEndToEnd walks the moderation flow (plan 7.4) over the scrubbed import through the
// production handler, every response checked against the spec. The steps run in order on one database; step 10
// runs before step 7, which blocks contributor A.
func TestHTTP_ModerationEndToEnd(t *testing.T) {
	t.Parallel()

	w := newModerationWorld(t)
	notesPiece, titlePiece, tokenPiece := w.pieces[0], w.pieces[1], w.pieces[2]
	const (
		addedNotes   = "Sung at every moderation revel."
		addedLinkURL = "https://example.org/moderation-e2e-recording"
		titleWord    = "Lanternwick"
		termName     = "Moderation Lantern Topic"
		newTitle     = "Moderation Lantern Ballad"
		// The contact request of step 9: personal data, which no log record may hold.
		senderName    = "Wren Placeholder"
		senderEmail   = "wren@example.org"
		senderMessage = "Please correct the second verse."
		reportText    = "The page is gone."
	)

	t.Run("1_2_stale_approve_rebases", func(t *testing.T) {
		start := w.editable(t, w.a, "pieces", notesPiece.Slug)
		require.Equal(t, notesPiece.Revision.ID, objectID(t, start, "base_revision_id"))
		notes := w.submitPieceEdit(t, w.a, notesPiece.Slug, notesPiece.Revision.ID,
			editedDocument(t, start, jsonObject{notesField: addedNotes}), "Add notes.")
		linksStart := w.editable(t, w.b, "pieces", notesPiece.Slug)
		links := append(append([]any{}, documentLinks(t, editedDocument(t, linksStart, nil))...), jsonObject{
			urlField: addedLinkURL, titleField: nil, "description": nil, "platform": nil, "place_type": nil,
			collectionIDParam: nil, "locator": nil, rolesField: []any{domain.LinkRoleText}, "performances": []any{}})
		linked := w.submitPieceEdit(t, w.b, notesPiece.Slug, notesPiece.Revision.ID,
			editedDocument(t, linksStart, jsonObject{linksField: links}), "Add a recording.")
		notesID, linkedID := objectID(t, notes, "id"), objectID(t, linked, "id")

		queue := sendOK[jsonObject](t, w.e, http.MethodGet, moderationQueuePath, w.moderator, "", http.StatusOK)
		assert.Contains(t, encodeJSON(t, queue["data"]), `"id":`+encodeJSON(t, notesID))
		assert.Contains(t, encodeJSON(t, queue["data"]), `"id":`+encodeJSON(t, linkedID))

		w.approve(t, notesID)
		stale := w.view(t, linkedID)
		assert.Equal(t, true, stale["stale"])
		rebase, ok := stale["rebase"].(jsonObject)
		require.True(t, ok, "the view has a rebase preview")
		assert.Equal(t, "clean", rebase["status"])
		w.approve(t, linkedID)

		public := w.publicPiece(t, notesPiece.Slug)
		assert.Contains(t, public, addedNotes)
		assert.Contains(t, public, addedLinkURL)
	})

	t.Run("3_conflict_rebase_approve", func(t *testing.T) {
		start := w.editable(t, w.a, "pieces", titlePiece.Slug)
		first := w.submitPieceEdit(t, w.a, titlePiece.Slug, titlePiece.Revision.ID,
			editedDocument(t, start, jsonObject{titleField: "First Moderation Title"}), "Retitle.")
		second := w.submitPieceEdit(t, w.b, titlePiece.Slug, titlePiece.Revision.ID,
			editedDocument(t, w.editable(t, w.b, "pieces", titlePiece.Slug),
				jsonObject{titleField: "Second Moderation Title"}), "Retitle differently.")
		secondID := objectID(t, second, "id")
		w.approve(t, objectID(t, first, "id"))
		sendRejected(t, w.e, http.MethodPost, idPath("/v1/revisions/", secondID)+"/approve", w.moderator,
			w.approveBody(t, secondID), http.StatusConflict, httpjson.CodeRebaseRequired)

		conflicted := w.view(t, secondID)
		rebase, ok := conflicted["rebase"].(jsonObject)
		require.True(t, ok, "the view has a rebase preview")
		assert.Equal(t, "conflicts", rebase["status"])
		conflicts, ok := rebase["conflicts"].([]any)
		require.True(t, ok, "the rebase preview has conflicts")
		require.NotEmpty(t, conflicts)
		assert.Contains(t, encodeJSON(t, conflicts), `"path":"title"`)
		current, ok := conflicted["current_document"].(jsonObject)
		require.True(t, ok, "the view has the current document")
		resolved := editedDocument(t, jsonObject{documentField: current},
			jsonObject{titleField: "The " + titleWord + " Moderation Title"})
		rebased := sendOK[jsonObject](t, w.e, http.MethodPost, idPath("/v1/revisions/", secondID)+"/rebase",
			w.moderator, encodeJSON(t, jsonObject{entityTypeField: domain.EntityTypePiece,
				"current_revision_id": conflicted["current_revision_id"], documentField: resolved}),
			http.StatusOK)
		assert.Equal(t, string(domain.RevisionStatusPending), rebased["status"])
		w.approve(t, secondID)
		assert.Contains(t, w.publicPiece(t, titlePiece.Slug), "The "+titleWord+" Moderation Title")
	})

	t.Run("3_reference_conflict_in_editor_format", func(t *testing.T) {
		start := w.editable(t, w.a, "pieces", titlePiece.Slug)
		termSlug := originTermNotOn(t, w, editedDocument(t, start, nil))
		withTerm := func(part domain.PiecePart) jsonObject {
			terms, ok := editedDocument(t, start, nil)[termsField].([]any)
			require.True(t, ok, "the document has terms")
			terms = append(slices.Clone(terms), jsonObject{"term": termSlug, "is_primary": false, "part": part})
			return editedDocument(t, start, jsonObject{termsField: terms})
		}
		base := objectID(t, start, "base_revision_id")
		first := w.submitPieceEdit(t, w.a, titlePiece.Slug, base, withTerm(domain.PiecePartWords), "Tag the words.")
		second := w.submitPieceEdit(t, w.b, titlePiece.Slug, base, withTerm(domain.PiecePartTune), "Tag the tune.")
		secondID := objectID(t, second, "id")
		assert.Contains(t, encodeJSON(t, w.view(t, secondID)["changed_paths"]), `"terms[`+termSlug+`]"`)
		w.approve(t, objectID(t, first, "id"))

		stale := w.view(t, secondID)
		rebase, ok := stale["rebase"].(jsonObject)
		require.True(t, ok, "the view has a rebase preview")
		require.Equal(t, "conflicts", rebase["status"])
		conflicts := encodeJSON(t, rebase["conflicts"])
		assert.Contains(t, conflicts, `"path":"terms[`+termSlug+`]"`, "the term by slug, as the editor format has it")
		assert.Contains(t, conflicts, `\"term\":\"`+termSlug+`\"`, "the values in the editor format")
		assert.NotContains(t, conflicts, `_id`, "no internal ID or stored field name")
		assert.NotContains(t, encodeJSON(t, stale["changed_paths"]), `_id`)
		assert.Contains(t, encodeJSON(t, stale["references"]), `"slug":"`+termSlug+`"`,
			"the conflict's slug is one of the view's references")
		w.reject(t, secondID)
	})

	t.Run("3_moderator_term_name_taken", func(t *testing.T) {
		terms := sendOK[jsonObject](t, w.e, http.MethodGet, "/v1/terms?facet="+string(domain.FacetTopic), "", "",
			http.StatusOK)
		list, ok := terms["data"].([]any)
		require.True(t, ok, "the term list has data")
		require.NotEmpty(t, list, "the scrubbed import has topic terms")
		taken, ok := list[0].(jsonObject)["name"].(string)
		require.True(t, ok, "a term has a name")

		draft := saveDraft(t, w.e, w.moderator, draftRequest(t, domain.EntityTypeTerm, nil, nil,
			newTermDocument(taken), "A topic named as another."))
		draftID := objectID(t, draft, "id")
		body := encodeJSON(t, jsonObject{"draft_id": draftID, "acknowledge_duplicates": true,
			"acknowledged_flags": []string{string(domain.FlagPossibleDuplicate)}})
		sendRejected(t, w.e, http.MethodPost, "/v1/revisions", w.moderator, body, http.StatusConflict,
			httpjson.CodeDuplicateName)
		rec := w.e.send(t, http.MethodDelete, idPath("/v1/drafts/", draftID), w.moderator, "")
		require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	})

	t.Run("4_pending_term_approved_with_piece", func(t *testing.T) {
		term := w.submitNew(t, w.a, domain.EntityTypeTerm, newTermDocument(termName), "Suggest a topic.")
		placeholder, ok := entityOf(t, term)["slug"].(string)
		require.True(t, ok, "the pending term has a placeholder slug")
		document := newPieceDocument(newTitle)
		document[termsField] = []any{jsonObject{"term": placeholder, "is_primary": true, "part": nil}}
		piece := w.submitNew(t, w.a, domain.EntityTypePiece, document, "A new piece.")

		approved := w.approve(t, objectID(t, piece, "id"))
		pieceSlug, ok := entityOf(t, approved)["slug"].(string)
		require.True(t, ok, "the approved piece has a slug")
		termView := w.view(t, objectID(t, term, "id"))
		assert.Equal(t, string(domain.RevisionStatusApproved), termView["status"])
		termSlug, ok := entityOf(t, termView)["slug"].(string)
		require.True(t, ok, "the approved term has a slug")

		public := sendOK[jsonObject](t, w.e, http.MethodGet, "/v1/terms/"+termSlug, "", "", http.StatusOK)
		assert.Equal(t, termName, public["name"])
		assert.InDelta(t, 1, public["piece_count"], 0)
		assert.Contains(t, encodeJSON(t, public["pieces"]), `"slug":"`+pieceSlug+`"`)
		tagged := sendOK[jsonObject](t, w.e, http.MethodGet, piecesPath+"?term="+termSlug, "", "", http.StatusOK)
		assert.InDelta(t, 1, tagged["total"], 0)
		assert.Contains(t, encodeJSON(t, tagged["data"]), `"slug":"`+pieceSlug+`"`)
	})

	t.Run("5_report_hide_revert", func(t *testing.T) {
		links := documentLinks(t, editedDocument(t, w.editable(t, w.a, "pieces", notesPiece.Slug), nil))
		reported, ok := links[0].(jsonObject)[urlField].(string)
		require.True(t, ok, "the piece's first link has a URL")
		require.NotEqual(t, addedLinkURL, reported)
		body := encodeJSON(t, jsonObject{urlField: reported, "reason": "broken", "message": reportText})
		receipt := sendOK[jsonObject](t, w.e, http.MethodPost, reportLinkPath, w.a, body, http.StatusCreated)
		assert.Equal(t, "open", receipt["status"])
		again := sendOK[jsonObject](t, w.e, http.MethodPost, reportLinkPath, w.a, body, http.StatusOK)
		reportID := objectID(t, receipt, "id")
		assert.Equal(t, reportID, objectID(t, again, "id"))

		open := sendOK[jsonObject](t, w.e, http.MethodGet, moderationReportsPath+"?status=open", w.moderator, "",
			http.StatusOK)
		listed, ok := open["data"].([]any)
		require.True(t, ok, "the inbox has data")
		require.True(t, slices.ContainsFunc(listed, func(r any) bool {
			report, ok := r.(jsonObject)
			return ok && objectID(t, report, "id") == reportID
		}), "the open report is listed")
		resolved := sendOK[jsonObject](t, w.e, http.MethodPost, idPath(moderationReportsPath+"/", reportID),
			w.moderator, `{"status":"resolved","note":null,"hide_link":true}`, http.StatusOK)
		assert.Equal(t, "resolved", resolved["status"])
		link, ok := resolved["link"].(jsonObject)
		require.True(t, ok, "the report has its link")
		assert.Equal(t, true, link["is_hidden"])
		assert.NotContains(t, w.publicPiece(t, notesPiece.Slug), encodeJSON(t, reported))

		reverted := sendOK[jsonObject](t, w.e, http.MethodPost,
			idPath("/v1/revisions/", notesPiece.Revision.ID)+"/revert", w.moderator,
			`{"summary":null,"acknowledged_flags":[]}`, http.StatusCreated)
		assert.Equal(t, string(domain.RevisionStatusApproved), reverted["status"])
		public := w.publicPiece(t, notesPiece.Slug)
		assert.NotContains(t, public, addedNotes, "the revert is published")
		assert.NotContains(t, public, encodeJSON(t, reported), "the reverted piece keeps the link hidden")
	})

	t.Run("6_reject_new_person", func(t *testing.T) {
		person := w.submitNew(t, w.a, domain.EntityTypePerson, newPersonDocument("Wren Placeholder Newcomer"),
			"A new bard.")
		placeholder, ok := entityOf(t, person)["slug"].(string)
		require.True(t, ok, "the pending person has a placeholder slug")
		w.editable(t, w.a, "persons", placeholder)

		id := objectID(t, person, "id")
		rejected := w.reject(t, id)
		assert.Nil(t, rejected["entity"])
		assert.Nil(t, w.view(t, id)["entity"])
		sendRejected(t, w.e, http.MethodGet, editablePath("persons", placeholder), w.a, "", http.StatusNotFound,
			httpjson.CodeNotFound)
	})

	t.Run("8_search_finds_approved_title", func(t *testing.T) {
		found := sendOK[jsonObject](t, w.e, http.MethodGet, piecesPath+"?q="+url.QueryEscape(titleWord), "", "",
			http.StatusOK)
		assert.Contains(t, encodeJSON(t, found["data"]), `"slug":"`+titlePiece.Slug+`"`)
	})

	t.Run("9_moderator_api_token_is_a_contributor", func(t *testing.T) {
		start := w.editable(t, w.modToken, "pieces", tokenPiece.Slug)
		submitted := w.submitPieceEdit(t, w.modToken, tokenPiece.Slug,
			start["base_revision_id"], editedDocument(t, start, jsonObject{notesField: "Scripted notes."}),
			"Scripted edit.")
		assert.Equal(t, string(domain.RevisionStatusPending), submitted["status"])
		id := objectID(t, submitted, "id")
		mine := sendOK[jsonObject](t, w.e, http.MethodGet, idPath("/v1/me/revisions/", id), w.modToken, "",
			http.StatusOK)
		assert.Equal(t, string(domain.RevisionStatusPending), mine["status"])

		sendRejected(t, w.e, http.MethodGet, moderationQueuePath, w.modToken, "", http.StatusForbidden,
			httpjson.CodeAPIAuthNotAllowed)
		sendRejected(t, w.e, http.MethodPost, idPath("/v1/revisions/", id)+"/approve", w.modToken,
			`{"acknowledged_flags":[]}`, http.StatusForbidden, httpjson.CodeAPIAuthNotAllowed)

		// The contact inbox holds personal data, which only an interactive session may read or act on.
		sendContact(t, w.e, contactBody("correction", senderName, senderEmail, senderMessage))
		sendRejected(t, w.e, http.MethodGet, contactRequestsPath, w.modToken, "", http.StatusForbidden,
			httpjson.CodeAPIAuthNotAllowed)
		inbox := sendOK[jsonObject](t, w.e, http.MethodGet, contactRequestsPath+"?status=open", w.moderator, "",
			http.StatusOK)
		request := contactRequestWith(t, inbox, senderMessage)
		assert.Equal(t, senderEmail, request["email"])
		resolvePath := idPath(contactRequestsPath+"/", objectID(t, request, "id"))
		sendRejected(t, w.e, http.MethodPost, resolvePath, w.modToken, `{"status":"resolved"}`,
			http.StatusForbidden, httpjson.CodeAPIAuthNotAllowed)
		resolved := sendOK[controller.ContactRequest](t, w.e, http.MethodPost, resolvePath, w.moderator,
			`{"status":"resolved","note":"Corrected."}`, http.StatusOK)
		assert.Equal(t, domain.ContactRequestStatusResolved, resolved.Status)
		sendRejected(t, w.e, http.MethodPost, resolvePath, w.moderator, `{"status":"dismissed"}`,
			http.StatusConflict, httpjson.CodeConflict)
	})

	t.Run("10_rename_to_hidden_alias", func(t *testing.T) {
		start := w.editable(t, w.a, "persons", w.renamed.Slug)
		rename := func(name string) (int64, []string) {
			t.Helper()
			draft := saveDraft(t, w.e, w.a, draftRequest(t, domain.EntityTypePerson, entitySlug(w.renamed.Slug),
				start["base_revision_id"], editedDocument(t, start, jsonObject{nameField: name}), "Use the usual name."))
			submitted := w.e.send(t, http.MethodPost, revisionsPath, w.a, submitBody(objectID(t, draft, "id")))
			require.Equal(t, http.StatusCreated, submitted.Code, submitted.Body.String())
			var revision jsonObject
			require.NoError(t, json.Unmarshal(submitted.Body.Bytes(), &revision))
			id := objectID(t, revision, "id")
			view := w.e.send(t, http.MethodGet, idPath("/v1/me/revisions/", id), w.a, "")
			require.Equal(t, http.StatusOK, view.Code, view.Body.String())
			return id, []string{submitted.Body.String(), view.Body.String()}
		}

		// The fresh rename is withdrawn, so that the rename to the hidden alias is pending on the same base.
		const fresh = "Wren Placeholder Fresh Name"
		freshID, freshBodies := rename(fresh)
		sendOK[jsonObject](t, w.e, http.MethodPost, idPath("/v1/revisions/", freshID)+"/withdraw", w.a, "",
			http.StatusOK)
		aliasID, aliasBodies := rename(w.renamedAlias)
		for i := range aliasBodies {
			assert.Equal(t, maskedBody(t, freshBodies[i], fresh), maskedBody(t, aliasBodies[i], w.renamedAlias),
				"a rename to a hidden alias is told apart from one to a fresh name")
			assert.NotContains(t, aliasBodies[i], "hidden_data_conflict")
		}

		assert.Contains(t, flagCodesOf(t, encodeJSON(t, w.view(t, aliasID))), "hidden_data_conflict")
		sendRejected(t, w.e, http.MethodPost, idPath("/v1/revisions/", aliasID)+"/approve", w.moderator,
			w.approveBody(t, aliasID), http.StatusConflict, httpjson.CodeHiddenDataConflict)
		w.reject(t, aliasID)
	})

	t.Run("7_blocked_author", func(t *testing.T) {
		start := w.editable(t, w.a, "pieces", tokenPiece.Slug)
		draftBody := draftRequest(t, domain.EntityTypePiece, entitySlug(tokenPiece.Slug), start["base_revision_id"],
			editedDocument(t, start, jsonObject{notesField: "Blocked notes."}), "Blocked edit.")
		draftID := objectID(t, saveDraft(t, w.e, w.a, draftBody), "id")
		w.block(t, w.moderator, moderationE2EAuthorPath)

		for _, req := range []struct{ method, path, body string }{
			{http.MethodPost, draftsPath, draftBody},
			{http.MethodPost, revisionsPath, submitBody(draftID)},
			{http.MethodPost, reportLinkPath, `{"url":"` + addedLinkURL + `","reason":"broken","message":null}`},
		} {
			sendRejected(t, w.e, req.method, req.path, w.a, req.body, http.StatusForbidden, httpjson.CodeUserBlocked)
		}
	})

	// No record the flow logged, at any level, holds a hidden alias of the import, the hidden alias A renamed the
	// person to, a link report's message or the contact request's personal data.
	w.logs.requireNoneOf(t, append([]string{w.renamedAlias, reportText, senderName, senderEmail, senderMessage},
		w.hiddenAliases...))
}
