//go:build integration

package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The marked strings the write cases' viewers type, which their own results show and no log record may hold.
const (
	// writeToolSummary is the summary of their drafts of wren and of their reverts of the piece.
	writeToolSummary = "leakmark-mcp-write-summary"
	// writeToolNotes are the notes of their repertoire entry of the piece.
	writeToolNotes = "leakmark-mcp-write-repertoire-notes"
)

// mcpWriteRun is one viewer's run of the write cases over the revision leak fixture.
type mcpWriteRun struct {
	w             revisionLeakWorld
	fx            revisionLeakFixture
	authorization string
	tools         map[openapitest.Operation]toolOperation
	// arguments collects the argument strings of every tool call, which no log record may hold.
	arguments *[]string
	// draft is the body of a POST /v1/drafts of the viewer's edit of wren, from their editable document.
	draft string
	// typed are the marked strings the viewer typed into their pending revision of wren, which their editable
	// document starts from, so their drafts of wren show them.
	typed []string
}

// mcpWriteCase is a write tool's case: run makes the tool's REST request and its tool call as the run's viewer, on
// twin state, and checks that they match and pass the leak scans.
type mcpWriteCase struct {
	tool string
	run  func(t *testing.T, r mcpWriteRun)
}

// mcpWriteCases are the write tools' cases, in the order they run. Where the REST request leaves the state the
// tool call needs (a draft to replace, an entry to update, a replayed revert), the two are made in turn; otherwise
// the tool call is made on a twin the run creates over REST first. Each result is compared with the REST response,
// IDs and times aside (assertWriteMatchesREST), and scanned for marked strings. report_link's case is
// assertHiddenLinkToolReportIsUnknown, of the hidden link.
var mcpWriteCases = []mcpWriteCase{
	{tool: "create_draft", run: func(t *testing.T, r mcpWriteRun) {
		// The tool's draft replaces the REST one: an author holds one draft per entity.
		rest := r.rest(t, http.MethodPost, draftsPath, r.draft)
		r.check(t, rest, r.tool(t, http.MethodPost, draftsPath, draftsPath, r.draft), r.wrenReveals()...)
		r.discard(t, objectID(t, decoded(t, rest), "id"))
	}},
	{tool: "update_draft", run: func(t *testing.T, r mcpWriteRun) {
		id := r.newDraft(t)
		// The body of a PUT is the draft's, without the entity and base, which a draft keeps.
		var draft jsonObject
		require.NoError(t, json.Unmarshal([]byte(r.draft), &draft))
		delete(draft, "entity")
		delete(draft, "base_revision_id")
		update := encodeJSON(t, draft)
		path := idPath("/v1/drafts/", id)
		rest := r.rest(t, http.MethodPut, path, update)
		r.check(t, rest, r.tool(t, http.MethodPut, draftPath, path, update), r.wrenReveals()...)
		r.discard(t, id)
	}},
	{tool: "delete_draft", run: func(t *testing.T, r mcpWriteRun) {
		rest := r.rest(t, http.MethodDelete, idPath("/v1/drafts/", r.newDraft(t)), "")
		path := idPath("/v1/drafts/", r.newDraft(t))
		r.check(t, rest, r.tool(t, http.MethodDelete, draftPath, path, ""))
	}},
	{tool: "submit_draft", run: func(t *testing.T, r mcpWriteRun) {
		// Each submission supersedes the viewer's pending revision of wren before it.
		rest := r.rest(t, http.MethodPost, revisionsPath, submitBody(r.newDraft(t)))
		body := submitBody(r.newDraft(t))
		r.check(t, rest, r.tool(t, http.MethodPost, revisionsPath, revisionsPath, body), r.wrenReveals()...)
	}},
	{tool: "withdraw_revision", run: func(t *testing.T, r mcpWriteRun) {
		rest := r.rest(t, http.MethodPost, idPath("/v1/revisions/", r.newRevision(t))+"/withdraw", "")
		path := idPath("/v1/revisions/", r.newRevision(t)) + "/withdraw"
		r.check(t, rest, r.tool(t, http.MethodPost, withdrawRoute, path, ""), r.wrenReveals()...)
	}},
	{tool: "revert_revision", run: func(t *testing.T, r mcpWriteRun) {
		// The tool's revert is the REST one replayed: identical to the viewer's pending revert of the piece. Its base
		// view is the piece's current version, which has A's approved notes.
		path := idPath("/v1/revisions/", r.fx.piece.RevisionID) + "/revert"
		body := `{"summary":"` + writeToolSummary + `","acknowledged_flags":[]}`
		rest := r.rest(t, http.MethodPost, path, body)
		r.check(t, rest, r.tool(t, http.MethodPost, revertRoute, path, body), writeToolSummary,
			pendingNotes)
	}},
	{tool: "set_repertoire_entry", run: func(t *testing.T, r mcpWriteRun) {
		path := repertoireEntryPath(r.fx.piece.Slug)
		body := `{"notes":"` + writeToolNotes + `","visibility":"private"}`
		rest := r.rest(t, http.MethodPut, path, body)
		r.check(t, rest, r.tool(t, http.MethodPut, repertoireEntryRoute, path, body), writeToolNotes)
	}},
	{tool: "remove_repertoire_entry", run: func(t *testing.T, r mcpWriteRun) {
		path := repertoireEntryPath(r.fx.piece.Slug)
		body := `{"notes":"` + writeToolNotes + `","visibility":"private"}`
		rest := r.rest(t, http.MethodDelete, path, "")
		sendOK[jsonObject](t, r.w.e, http.MethodPut, path, r.authorization, body, http.StatusCreated)
		r.check(t, rest, r.tool(t, http.MethodDelete, repertoireEntryRoute, path, ""))
	}},
	{tool: "report_link", run: func(t *testing.T, r mcpWriteRun) {
		t.Helper()
		if r.authorization == r.w.viewers[viewerAuthorToken] {
			r.w.assertHiddenLinkToolReportIsUnknown(t)
		}
	}},
}

// assertWriteToolsMatchREST runs the write cases as A's API token (AT) and the moderator's (MT), after the
// moderator approves A's pending edit of the piece, so that the piece has an earlier approved revision to revert
// to. It returns the argument strings of every tool call.
func (w revisionLeakWorld) assertWriteToolsMatchREST(t *testing.T, fx revisionLeakFixture) []string {
	t.Helper()

	sendOK[jsonObject](t, w.e, http.MethodPost, idPath("/v1/revisions/", fx.pieceEdit)+"/approve",
		w.viewers[viewerModerator], `{"acknowledged_flags":[]}`, http.StatusOK)
	var arguments []string
	for _, viewer := range []revisionViewer{viewerAuthorToken, viewerModeratorToken} {
		authorization := w.viewers[viewer]
		editable := sendOK[jsonObject](t, w.e, http.MethodGet, editablePath("persons", fx.wren.Slug), authorization,
			"", http.StatusOK)
		var typed []string
		if viewer == viewerAuthorToken {
			typed = typedByAuthor
		}
		run := mcpWriteRun{w: w, fx: fx, authorization: authorization, tools: toolsByOperation(t),
			typed:     typed,
			arguments: &arguments, draft: draftRequest(t, domain.EntityTypePerson, entitySlug(fx.wren.Slug),
				editable["base_revision_id"], editedDocument(t, editable, jsonObject{notesField: "Sings at moots."}),
				writeToolSummary)}
		for _, c := range mcpWriteCases {
			t.Run(string(viewer)+" "+c.tool, func(t *testing.T) { c.run(t, run) })
		}
	}

	return arguments
}

// rest sends a REST request as the run's viewer.
func (r mcpWriteRun) rest(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	return r.w.e.send(t, method, path, r.authorization, body)
}

// tool calls, as the run's viewer, the tool of the operation method template with the arguments of the REST
// request to target with body, and records their strings.
func (r mcpWriteRun) tool(t *testing.T, method, template, target, body string) mcpToolResult {
	t.Helper()

	to, ok := r.tools[openapitest.Operation{Method: method, Path: template}]
	require.True(t, ok, "no tool calls %s %s", method, template)
	args := toolArguments(t, to, target, body)
	*r.arguments = append(*r.arguments, argumentStrings(t, args)...)

	return r.w.e.callTool(t, r.authorization, to.tool.Name, args)
}

// check checks that result matches the REST response rest and that the result shows reveals and no other marked
// string.
func (r mcpWriteRun) check(t *testing.T, rest *httptest.ResponseRecorder, result mcpToolResult, reveals ...string) {
	t.Helper()

	if body := assertWriteMatchesREST(t, rest, result); body != "" {
		assertNoLeakIn(t, body, reveals)
	}
}

// newDraft saves the run's draft of wren over REST and returns its ID.
func (r mcpWriteRun) newDraft(t *testing.T) int64 {
	t.Helper()

	return objectID(t, sendOK[jsonObject](t, r.w.e, http.MethodPost, draftsPath, r.authorization, r.draft,
		http.StatusCreated), "id")
}

// newRevision submits a new draft of wren over REST and returns the pending revision's ID.
func (r mcpWriteRun) newRevision(t *testing.T) int64 {
	t.Helper()

	return objectID(t, submitDraft(t, r.w.e, r.authorization, r.newDraft(t)), "id")
}

// discard deletes the draft id over REST.
func (r mcpWriteRun) discard(t *testing.T, id int64) {
	t.Helper()

	rec := r.rest(t, http.MethodDelete, idPath("/v1/drafts/", id), "")
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
}

// decoded is the JSON object of a response.
func decoded(t *testing.T, rec *httptest.ResponseRecorder) jsonObject {
	t.Helper()

	var o jsonObject
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &o), rec.Body.String())

	return o
}

// assertWriteMatchesREST checks that the result of a write tool's call matches rest, the REST response to the same
// write on twin state: a 2xx body as the text and the structured content, with the values of IDs and times masked
// (maskedFields), since the two writes made different rows at different times; otherwise as
// assertToolMatchesREST. It returns the tool's text for a 2xx body, else the REST body.
func assertWriteMatchesREST(t *testing.T, rest *httptest.ResponseRecorder, result mcpToolResult) string {
	t.Helper()

	body := strings.TrimSpace(rest.Body.String())
	if rest.Code < http.StatusOK || rest.Code >= http.StatusMultipleChoices || body == "" {
		return assertToolMatchesREST(t, rest, result)
	}
	require.False(t, result.IsError, result.text(t))
	text := result.text(t)
	assert.Equal(t, maskedBody(t, body, writeToolSummary), maskedBody(t, text, writeToolSummary),
		"the tool's text is not the REST body, IDs and times aside")
	assert.JSONEq(t, text, string(result.StructuredContent), "the structured content is not the text")

	return text
}

// TestMCP_EveryToolHasLeakCase checks that every MCP tool has a case in a leak matrix: a public read tool a
// leakMatrix row of each operation (TestHTTP_MCPLeakMatrix), an authenticated read tool a revisionLeakMatrix row
// of each (TestHTTP_MCPRevisionLeakMatrix), and a write tool a case in mcpWriteCases.
func TestMCP_EveryToolHasLeakCase(t *testing.T) {
	t.Parallel()

	contract := openapitest.New(t)
	var missing []string
	for op, to := range toolsByOperation(t) {
		var covered bool
		switch {
		case op.Method != http.MethodGet:
			covered = slices.ContainsFunc(mcpWriteCases, func(c mcpWriteCase) bool { return c.tool == to.tool.Name })
		case contract.Public(op):
			_, covered = leakMatrix[op]
		default:
			_, covered = revisionLeakMatrix[op]
		}
		if !covered {
			missing = append(missing, to.tool.Name)
		}
	}
	slices.Sort(missing)
	assert.Empty(t, slices.Compact(missing), "these tools have no leak case")
}

// wrenReveals are the marked strings the run's drafts and revisions of wren show: their summary, and what the
// viewer typed into the pending revision their editable document starts from.
func (r mcpWriteRun) wrenReveals() []string {
	return append([]string{writeToolSummary}, r.typed...)
}
