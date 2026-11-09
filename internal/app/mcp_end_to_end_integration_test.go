//go:build integration

package app

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/transport/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mcpE2EModeratorID   = "facebook|mcp-e2e-moderator"
	mcpE2EContributorID = "google-oauth2|mcp-e2e-contributor"
)

// mcpE2EDescription is the description the contributor's revision gives the event.
const mcpE2EDescription = "A harvest gathering by the river."

// structured decodes a successful tool result's structured content.
func structured(t *testing.T, result mcpToolResult) jsonObject {
	t.Helper()

	require.False(t, result.IsError, result.text(t))
	var decoded jsonObject
	require.NoError(t, json.Unmarshal(result.StructuredContent, &decoded))
	return decoded
}

// TestHTTP_MCPEndToEnd drives the MCP server over a real database as an agent would: it lists the tools, reads an
// event, and as a contributor's API token edits it through get_editable, create_draft and submit_draft, reads its
// own pending revision and finds it in its revisions. A moderator's token proposes a new term, which stays pending
// where the moderator's Auth0 session would approve it at once. Once the moderator approves the contributor's
// edit over REST, get_event shows it.
func TestHTTP_MCPEndToEnd(t *testing.T) {
	t.Parallel()

	w := newRevisionWorld(t, nil, mcpE2EModeratorID)
	moot := catalogtest.New(t, w.db).Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Harvest Moot"}})
	contributor := w.apiToken(t, w.auth0(t, mcpE2EContributorID))
	moderator := w.auth0(t, mcpE2EModeratorID)
	moderatorToken := w.apiToken(t, moderator)

	rec := w.e.sendMCP(t, contributor, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var listed struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &listed))
	var names, want []string
	for _, tool := range listed.Result.Tools {
		names = append(names, tool.Name)
	}
	for _, tool := range testMCPTools(t).Tools() {
		want = append(want, tool.Name)
	}
	assert.ElementsMatch(t, want, names)

	event := structured(t, w.e.callTool(t, contributor, "get_event", map[string]any{slugParam: moot.Slug}))
	assert.Equal(t, "Harvest Moot", event[nameField])

	editable := structured(t, w.e.callTool(t, contributor, "get_editable",
		map[string]any{entityTypeArgument: "event", slugParam: moot.Slug}))
	noLinkStates(t, encodeJSON(t, editable))
	draft := structured(t, w.e.callTool(t, contributor, "create_draft", map[string]any{bodyArgument: json.RawMessage(
		draftRequest(t, domain.EntityTypeEvent, entitySlug(moot.Slug), nil,
			editedDocument(t, editable, jsonObject{descriptionField: mcpE2EDescription}), "Describe the moot."))}))
	submitted := structured(t, w.e.callTool(t, contributor, "submit_draft",
		map[string]any{bodyArgument: json.RawMessage(submitBody(objectID(t, draft, "id")))}))
	assert.Equal(t, string(domain.RevisionStatusPending), submitted[statusField])
	revisionID := objectID(t, submitted, "id")

	pending := structured(t, w.e.callTool(t, contributor, "get_pending",
		map[string]any{entityTypeArgument: "event", slugParam: moot.Slug}))
	assert.Equal(t, revisionID, objectID(t, pending, "id"))
	assert.Contains(t, encodeJSON(t, pending), mcpE2EDescription)

	mine := structured(t, w.e.callTool(t, contributor, "list_my_revisions", map[string]any{}))
	assert.Contains(t, revisionIDs(t, mine), revisionID)
	assert.NotContains(t, revisionIDs(t, structured(t, w.e.callTool(t, moderatorToken, "list_my_revisions",
		map[string]any{}))), revisionID, "another user's token lists the contributor's revision")

	termDraft := structured(t, w.e.callTool(t, moderatorToken, "create_draft", map[string]any{
		bodyArgument: json.RawMessage(draftRequest(t, domain.EntityTypeTerm, nil, nil, newTermDocument("Harvests"),
			"A new topic.")),
	}))
	term := structured(t, w.e.callTool(t, moderatorToken, "submit_draft",
		map[string]any{bodyArgument: json.RawMessage(submitBody(objectID(t, termDraft, "id")))}))
	assert.Equal(t, string(domain.RevisionStatusPending), term[statusField],
		"a moderator's token submits like a contributor")

	approved := sendOK[jsonObject](t, w.e, http.MethodPost, idPath("/v1/revisions/", revisionID)+"/approve",
		moderator, `{"acknowledged_flags":[]}`, http.StatusOK)
	assert.Equal(t, string(domain.RevisionStatusApproved), approved[statusField])
	event = structured(t, w.e.callTool(t, contributor, "get_event", map[string]any{slugParam: moot.Slug}))
	assert.Equal(t, mcpE2EDescription, event[descriptionField])

	assertRevisionSources(t, w, contributor, moderator, moot, revisionID)
}

// assertRevisionSources checks the source of the contributor's revisions (docs/PLAN.md §4): revisionID, whose
// draft the contributor created over MCP, is mcp, and so is their revert of moot over MCP; a draft created over
// REST is user, even when submitted over MCP, and so is a revert over REST.
func assertRevisionSources(t *testing.T, w revisionWorld, contributor, moderator string, moot catalogtest.Ref,
	revisionID int64,
) {
	t.Helper()

	sourceOf := func(id int64) any {
		viaMCP := structured(t, w.e.callTool(t, contributor, "get_my_revision", map[string]any{revisionIDParam: id}))
		viaREST := sendOK[jsonObject](t, w.e, http.MethodGet, idPath("/v1/me/revisions/", id), contributor, "",
			http.StatusOK)
		assert.Equal(t, viaREST["source"], viaMCP["source"])

		return viaMCP["source"]
	}
	revertBody := `{"summary":"Restore the first version.","acknowledged_flags":[]}`

	assert.Equal(t, string(domain.RevisionSourceMCP), sourceOf(revisionID), "a revision whose draft was created over MCP")
	reverted := structured(t, w.e.callTool(t, contributor, "revert_revision",
		map[string]any{revisionIDParam: moot.RevisionID, bodyArgument: json.RawMessage(revertBody)}))
	assert.Equal(t, string(domain.RevisionSourceMCP), sourceOf(objectID(t, reverted, "id")), "a revert over MCP")

	fair := catalogtest.New(t, w.db).Event(catalogtest.Event{Doc: domain.EventDocument{Name: "Autumn Fair"}})
	editable := sendOK[jsonObject](t, w.e, http.MethodGet, editablePath("events", fair.Slug), contributor, "",
		http.StatusOK)
	draft := saveDraft(t, w.e, contributor, draftRequest(t, domain.EntityTypeEvent, entitySlug(fair.Slug), nil,
		editedDocument(t, editable, jsonObject{descriptionField: "An autumn fair."}), "Describe the fair."))
	submitted := structured(t, w.e.callTool(t, contributor, "submit_draft",
		map[string]any{bodyArgument: json.RawMessage(submitBody(objectID(t, draft, "id")))}))
	fairEdit := objectID(t, submitted, "id")
	assert.Equal(t, string(domain.RevisionSourceUser), sourceOf(fairEdit),
		"a draft created over REST and submitted over MCP")

	sendOK[jsonObject](t, w.e, http.MethodPost, idPath("/v1/revisions/", fairEdit)+"/approve", moderator,
		`{"acknowledged_flags":[]}`, http.StatusOK)
	restRevert := sendOK[jsonObject](t, w.e, http.MethodPost, idPath("/v1/revisions/", fair.RevisionID)+"/revert",
		contributor, revertBody, http.StatusCreated)
	assert.Equal(t, string(domain.RevisionSourceUser), sourceOf(objectID(t, restRevert, "id")), "a revert over REST")
}

// revisionIDs are the IDs of a list of the caller's revisions.
func revisionIDs(t *testing.T, list jsonObject) []int64 {
	t.Helper()

	data, ok := list["data"].([]any)
	require.True(t, ok, "the list has no data: %v", list)
	ids := make([]int64, 0, len(data))
	for _, item := range data {
		revision, ok := item.(jsonObject)
		require.True(t, ok)
		ids = append(ids, objectID(t, revision, "id"))
	}
	return ids
}

// TestMCP_ArgumentsCannotReachOtherOperations calls tools as a contributor's API token with arguments that would
// address other routes if they were pasted into the path: each call answers with its own operation's result (not
// found) or refuses the arguments, and every request each call made was routed to that tool's own operation (or
// was the call itself, to /v1/mcp). An unknown slug is among them, so that some calls do reach an operation.
func TestMCP_ArgumentsCannotReachOtherOperations(t *testing.T) {
	t.Parallel()

	logs, logger := newLogCapture()
	w := newRevisionWorld(t, logger, "facebook|mcp-traversal-moderator")
	token := w.apiToken(t, w.auth0(t, "google-oauth2|mcp-traversal-contributor"))

	traversals := []string{"../moderation/queue", "x%2Feditable", "x/../../tokens", "a?include_blocked=true",
		"x/history", "..", ".", "no-such-piece"}
	type call struct {
		tool string
		args map[string]any
	}
	var calls []call
	for _, value := range traversals {
		calls = append(calls,
			call{"get_piece", map[string]any{slugParam: value}},
			call{"get_editable", map[string]any{entityTypeArgument: "piece", slugParam: value}},
			call{"get_pending", map[string]any{entityTypeArgument: "person", slugParam: value}},
			call{"get_my_repertoire_entry", map[string]any{slugParam: value}},
			call{"list_public_repertoire", map[string]any{"public_id": value}},
		)
	}
	calls = append(calls,
		call{"get_draft", map[string]any{"draft_id": "1/x"}},
		call{"get_revision", map[string]any{revisionIDParam: "1/../../moderation/queue"}},
		call{"get_collection", map[string]any{collectionIDParam: "1?x=y"}},
	)

	reached := false
	for _, c := range calls {
		own := map[string]bool{mcpPath: true}
		for _, op := range toolNamed(t, c.tool).Operations {
			own[op.Path] = true
		}
		t.Run(c.tool+" "+encodeJSON(t, c.args), func(t *testing.T) {
			before := len(logs.String())
			toolError := w.e.callTool(t, token, c.tool, c.args).toolError(t)
			switch toolError["code"] {
			case "not_found":
				assert.InDelta(t, http.StatusNotFound, toolError["status"], 0)
			case "invalid_request":
				assert.InDelta(t, http.StatusBadRequest, toolError["status"], 0)
			default:
				t.Errorf("neither the operation's not found nor a refusal: %v", toolError)
			}
			routes := loggedRoutes(t, logs.String()[before:])
			require.Contains(t, routes, mcpPath, "the call was not logged, so the routes checked prove nothing")
			for _, route := range routes {
				assert.True(t, own[route], "%s reached %s, not its own operation", c.tool, route)
			}
			reached = reached || slices.Contains(routes, pieceRoute)
		})
	}
	assert.True(t, reached, "no call reached an operation, so the routes checked prove nothing")
}

// loggedRoutes are the routes, and the tool routes, of the log records in logs.
func loggedRoutes(t *testing.T, logs string) []string {
	t.Helper()

	var routes []string
	for line := range strings.Lines(logs) {
		var record struct {
			Route     string `json:"route"`
			ToolRoute string `json:"tool_route"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		for _, route := range []string{record.Route, record.ToolRoute} {
			if route != "" {
				routes = append(routes, route)
			}
		}
	}

	return routes
}

// toolNamed returns the tool named name.
func toolNamed(t *testing.T, name string) mcp.Tool {
	t.Helper()

	for _, tool := range testMCPTools(t).Tools() {
		if tool.Name == name {
			return tool
		}
	}
	require.Failf(t, "no such tool", name)
	return mcp.Tool{}
}
