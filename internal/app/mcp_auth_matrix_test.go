package app

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// toolOperationCount is how many operations the tools call, as TestMCP_ToolTableCoversOnlyAllowedOperations pins.
const toolOperationCount = 40

// TestMCP_AuthMatrix calls every tool, once for each of its operations, as every principal, through the production
// handler. The expected outcome is derived: the route's from apiTokenUser, and for the principals it lets through
// (the API tokens) the inner operation's from that operation's authMatrix row, whose request the call makes. A
// route refusal is an HTTP error; an inner one is a tool error with the REST status and code. An allowed call
// acts as a contributor: a moderator's token submits a pending revision and gets the contributor's editable
// document, without link states.
func TestMCP_AuthMatrix(t *testing.T) {
	t.Parallel()

	byOperation := toolsByOperation(t)
	require.Len(t, byOperation, toolOperationCount)
	for op := range byOperation {
		row, ok := authMatrix[op]
		require.True(t, ok, "%s %s, which a tool calls, has no authMatrix row", op.Method, op.Path)
		for _, p := range []principal{apiAuthContributor, apiAuthModerator} {
			assert.NotEqual(t, apiAuthNotAllowed, row.access.outcomes[p],
				"%s %s refuses API tokens, so no tool may call it", op.Method, op.Path)
		}
	}

	for op, to := range byOperation {
		row := authMatrix[op]
		t.Run(to.tool.Name+" "+op.Method+" "+op.Path, func(t *testing.T) {
			t.Parallel()

			args := toolArguments(t, to, expandPath(op.Path, row.pathParams)+row.query, row.body)
			for _, p := range principals {
				t.Run(p.name, func(t *testing.T) {
					t.Parallel()
					assertToolOutcome(t, to, row, args, p)
				})
			}
		})
	}
}

// assertToolOutcome calls the tool of to with args as p, and checks the outcome the route and the inner operation's
// row derive.
func assertToolOutcome(t *testing.T, to toolOperation, row matrixRow, args map[string]any, p principal) {
	t.Helper()

	m := newHandlerMocks(t)
	if p.resolves != "" {
		m.expectUser(p.resolves)
	}
	route := apiTokenUser.outcomes[p]
	inner := row.access.outcomes[p]
	if route == allowed {
		if row.access.authenticated {
			// The inner request authenticates again.
			m.expectUser(p.resolves)
		}
		if inner == allowed {
			row.expectSuccess(m, p.resolves)
		}
	}

	rec := m.validate.Serve(t, m.handler, newMCPRequest(t.Context(), p.authorization,
		mcpToolCallMessage(t, to.tool.Name, args)))
	assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	if route != allowed {
		require.Equal(t, route.status, rec.Code, rec.Body.String())
		var body errorResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		assert.Equal(t, route.code, body.Code)
		return
	}

	result := decodeToolResult(t, rec)
	if inner != allowed {
		toolError := result.toolError(t)
		assert.InDelta(t, inner.status, toolError["status"], 0)
		assert.Equal(t, string(inner.code), toolError["code"])
		return
	}

	require.False(t, result.IsError, result.text(t))
	if row.successStatus == http.StatusNoContent {
		assert.JSONEq(t, `{"status":204}`, result.text(t))
		assert.Empty(t, result.StructuredContent)
	} else {
		assert.JSONEq(t, result.text(t), string(result.StructuredContent))
	}

	actors := recordedActors(m)
	for _, actor := range actors {
		assert.Equal(t, domain.Actor{UserID: p.resolves, Role: domain.RoleContributor}, actor,
			"an API token acts with a contributor's powers")
	}
	switch to.op.ID {
	case "submitRevision":
		require.NotEmpty(t, actors)
		var submitted struct {
			Status domain.RevisionStatus `json:"status"`
		}
		require.NoError(t, json.Unmarshal(result.StructuredContent, &submitted))
		assert.Equal(t, domain.RevisionStatusPending, submitted.Status)
	case "getPieceEditable", "getPersonEditable", "getTermEditable", "getEventEditable", "getCollectionEditable":
		require.NotEmpty(t, actors)
		noLinkStates(t, result.text(t))
	}
}

// recordedActors are the actors the handler gave the datasources that act for a user, directly or as a request's
// Actor field.
func recordedActors(m handlerMocks) []domain.Actor {
	var actors []domain.Actor
	add := func(argument any) {
		if actor, ok := argument.(domain.Actor); ok {
			actors = append(actors, actor)
			return
		}
		v := reflect.ValueOf(argument)
		if v.Kind() != reflect.Struct {
			return
		}
		if field := v.FieldByName("Actor"); field.IsValid() {
			if actor, ok := field.Interface().(domain.Actor); ok {
				actors = append(actors, actor)
			}
		}
	}

	for _, recorded := range writeAndOwnDataMocks(m) {
		for _, call := range recorded.Calls {
			for _, argument := range call.Arguments {
				add(argument)
			}
		}
	}
	return actors
}

// writeAndOwnDataMocks are the mocks of the datasources that write, or read the caller's own data.
func writeAndOwnDataMocks(m handlerMocks) []*mock.Mock {
	return []*mock.Mock{
		&m.refs.Mock, &m.editable.Mock, &m.draftSaver.Mock, &m.draftUpdater.Mock, &m.drafts.Mock,
		&m.draftDiscarder.Mock, &m.duplicates.Mock, &m.submitter.Mock, &m.withdrawer.Mock, &m.reverter.Mock,
		&m.myRevisionLister.Mock, &m.myRevisions.Mock, &m.pending.Mock, &m.reporter.Mock, &m.repertoire.Mock,
		&m.repertoireEntries.Mock, &m.repertoirePutter.Mock, &m.repertoireDeleter.Mock,
	}
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
