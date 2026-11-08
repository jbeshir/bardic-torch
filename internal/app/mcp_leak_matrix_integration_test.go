//go:build integration

package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources/mysql"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/catalogtest"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The users of the MCP leak matrix over the shared leak fixture. Their IDs are unmarked, since logs carry them.
const (
	mcpLeakContributorID = "google-oauth2|mcp-leak-contributor"
	mcpLeakModeratorID   = "google-oauth2|mcp-leak-moderator"
)

// sendMCP serves a POST /v1/mcp request with message as authorization, checking the exchange against the spec.
func (e endToEnd) sendMCP(t *testing.T, authorization, message string) *httptest.ResponseRecorder {
	t.Helper()

	ctx := t.Context()
	if e.logger != nil {
		ctx = domain.ContextWithLogger(ctx, e.logger)
	}
	return e.contract.Serve(t, e.handler, newMCPRequest(ctx, authorization, message))
}

// callTool calls tool with args as authorization and returns the result. The JSON-RPC envelope around the result's
// content holds no marked string.
func (e endToEnd) callTool(t *testing.T, authorization, tool string, args map[string]any) mcpToolResult {
	t.Helper()

	rec := e.sendMCP(t, authorization, mcpToolCallMessage(t, tool, args))
	result := decodeToolResult(t, rec)

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	if inner, ok := envelope["result"].(map[string]any); ok {
		delete(inner, "content")
		delete(inner, "structuredContent")
	}
	assert.Empty(t, leakedMarker(encodeJSON(t, envelope), nil), "the envelope shows a marked string")
	return result
}

// assertToolMatchesREST checks that a tool result is the REST response rest: a 2xx body as the text, byte for
// byte, and as the structured content; a 2xx without a body as its status; and an error as the REST error body
// with its status. It returns the REST body, which the leak scans then read as the tool's answer.
func assertToolMatchesREST(t *testing.T, rest *httptest.ResponseRecorder, result mcpToolResult) string {
	t.Helper()

	body := strings.TrimSpace(rest.Body.String())
	switch {
	case rest.Code >= http.StatusOK && rest.Code < http.StatusMultipleChoices && body == "":
		require.False(t, result.IsError, result.text(t))
		assert.JSONEq(t, encodeJSON(t, map[string]int{"status": rest.Code}), result.text(t))
	case rest.Code >= http.StatusOK && rest.Code < http.StatusMultipleChoices:
		require.False(t, result.IsError, result.text(t))
		assert.Equal(t, body, result.text(t), "the tool's text is not the REST body")
		assert.JSONEq(t, body, string(result.StructuredContent), "the structured content is not the REST body")
	default:
		toolError := result.toolError(t)
		assert.InDelta(t, rest.Code, toolError["status"], 0)
		delete(toolError, "status")
		delete(toolError, "retry_after_seconds")
		assert.JSONEq(t, body, encodeJSON(t, toolError), "the tool error is not the REST error")
	}
	return body
}

// argumentStrings are the string values in args, the body's included, long enough that no log line holds one
// by chance; entity_type is left out, as route templates name entity types.
func argumentStrings(t *testing.T, args map[string]any) []string {
	t.Helper()

	var values []string
	var collect func(v any)
	collect = func(v any) {
		switch v := v.(type) {
		case string:
			if len(v) >= 8 {
				values = append(values, v)
			}
		case []any:
			for _, e := range v {
				collect(e)
			}
		case map[string]any:
			for _, e := range v {
				collect(e)
			}
		}
	}
	for name, value := range args {
		switch name {
		case entityTypeArgument:
		case bodyArgument:
			var decoded any
			raw, ok := value.(json.RawMessage)
			require.True(t, ok)
			require.NoError(t, json.Unmarshal(raw, &decoded))
			collect(decoded)
		default:
			collect(value)
		}
	}
	return values
}

// requireNoneSince checks that no log record written after the first offset bytes holds any of values.
func (l *logCapture) requireNoneSince(t *testing.T, offset int, values []string) {
	t.Helper()

	logs := l.String()[offset:]
	require.Contains(t, logs, "mcp tool called", "no tool call was logged, so this check proves nothing")
	for line := range strings.Lines(logs) {
		for _, v := range values {
			assert.NotContains(t, line, v, "a log record holds a tool argument")
		}
	}
}

// TestHTTP_MCPLeakMatrix calls every public read tool with the arguments of every leakMatrix request of its
// operation, over the shared leak fixture, as a contributor's API token and a moderator's. Each result must be
// the REST response to the same request (assertToolMatchesREST), and passes the same scans; the records
// the handler logged hold no marked string and no argument value.
func TestHTTP_MCPLeakMatrix(t *testing.T) {
	t.Parallel()

	db, fx := leakCatalogue.Get(t)
	requireLeakMarkersStored(t, db)
	logs, logger := newLogCapture()
	w := revisionWorldOn(t, db, logger)
	tokens := fx.mcpTokens
	contract := openapitest.New(t)

	var arguments []string
	for op, to := range toolsByOperation(t) {
		if op.Method != http.MethodGet || !contract.Public(op) {
			continue
		}
		requests, ok := leakMatrix[op]
		require.True(t, ok, "%s %s, a public read a tool calls, has no leakMatrix row", op.Method, op.Path)

		t.Run(to.tool.Name+" "+op.Path, func(t *testing.T) {
			for _, req := range requests(fx) {
				args := toolArguments(t, to, req.path, "")
				arguments = append(arguments, argumentStrings(t, args)...)
				for viewer, authorization := range tokens {
					t.Run(string(viewer)+" "+req.path, func(t *testing.T) {
						rest := w.e.send(t, http.MethodGet, req.path, authorization, "")
						require.Equal(t, req.status, rest.Code, rest.Body.String())
						body := assertToolMatchesREST(t, rest, w.e.callTool(t, authorization, to.tool.Name, args))
						assertNoLeakInBody(t, body, req)
						if req.sameBodyAs != "" {
							other := w.e.send(t, http.MethodGet, req.sameBodyAs, authorization, "")
							assert.Equal(t, strings.TrimSpace(other.Body.String()), body,
								"the response is told apart from %s's", req.sameBodyAs)
						}
					})
				}
			}
		})
	}

	logs.requireNoMarkedLogs(t)
	logs.requireNoneSince(t, 0, arguments)
}

// TestHTTP_MCPRevisionLeakMatrix calls every authenticated read tool with the arguments of every
// revisionLeakMatrix request of its operation, over the revision leak fixture. A's API token (AT) and the
// moderator's (MT) get the REST response to the same request, which passes the same checks, so MT gets the
// contributor projection; every other viewer, and the blocked user, is refused by the route. A hidden link's
// report is not found, byte for byte as a URL no link has. No record the handler logged holds a marked string or
// an argument value.
func TestHTTP_MCPRevisionLeakMatrix(t *testing.T) {
	t.Parallel()

	w, fx := newRevisionLeakWorld(t)
	requireRevisionLeakMarkersStored(t, w, fx)
	contract := openapitest.New(t)
	offset := len(w.logs.String())

	var arguments []string
	for op, to := range toolsByOperation(t) {
		if op.Method != http.MethodGet || contract.Public(op) {
			continue
		}
		rows, ok := revisionLeakMatrix[op]
		require.True(t, ok, "%s %s, an authenticated read a tool calls, has no revisionLeakMatrix row", op.Method,
			op.Path)

		t.Run(to.tool.Name+" "+op.Path, func(t *testing.T) {
			tokenRows := 0
			for _, req := range withAuthorToken(rows(fx)) {
				args := toolArguments(t, to, req.path, "")
				arguments = append(arguments, argumentStrings(t, args)...)
				t.Run(string(req.viewer)+" "+req.path, func(t *testing.T) {
					if req.viewer != viewerAuthorToken && req.viewer != viewerModeratorToken {
						rec := w.e.sendMCP(t, w.viewers[req.viewer], mcpToolCallMessage(t, to.tool.Name, args))
						require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
						requireErrorBodyOnly(t, rec.Body.String(), httpjson.CodeAPIAuthRequired)
						return
					}
					tokenRows++
					w.assertToolRow(t, to, req, args)
				})
			}
			assert.Positive(t, tokenRows, "no row reads %s as an API token", op.Path)
		})
	}

	w.assertHiddenLinkToolReportIsUnknown(t)
	w.logs.requireNoMarkedLogs(t)
	w.logs.requireNoneSince(t, offset, arguments)
}

// assertToolRow calls the tool of to with args as req's viewer, and checks that the result is the REST response
// to req, which passes req's checks; then that the blocked user's credentials get only the route's user_blocked
// error.
func (w revisionLeakWorld) assertToolRow(t *testing.T, to toolOperation, req revisionLeakRequest, args map[string]any) {
	t.Helper()

	authorization := w.viewers[req.viewer]
	rest := w.e.send(t, http.MethodGet, req.path, authorization, "")
	require.Equal(t, req.status, rest.Code, rest.Body.String())
	body := assertToolMatchesREST(t, rest, w.e.callTool(t, authorization, to.tool.Name, args))
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
		rec := w.e.sendMCP(t, blocked, mcpToolCallMessage(t, to.tool.Name, args))
		require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
		requireErrorBodyOnly(t, rec.Body.String(), httpjson.CodeUserBlocked)
	}
}

// assertHiddenLinkToolReportIsUnknown checks that AT's report_link of the hidden link is the same tool error as a
// report of a URL no link has.
func (w revisionLeakWorld) assertHiddenLinkToolReportIsUnknown(t *testing.T) {
	t.Helper()

	report := func(linkURL string) map[string]any {
		result := w.e.callTool(t, w.viewers[viewerAuthorToken], "report_link",
			map[string]any{bodyArgument: json.RawMessage(reportLinkRequest(t, linkURL, "Broken."))})
		toolError := result.toolError(t)
		assert.InDelta(t, http.StatusNotFound, toolError["status"], 0)
		assert.Empty(t, leakedMarker(result.text(t), nil))
		return toolError
	}
	assert.Equal(t, report("https://example.org/no-such-link"), report(hiddenLinkURL),
		"a hidden link's report is told apart from no link's")
}

// seedMCPLeakTokens creates, while the shared leak fixture is seeded, a contributor's API token (AT) and a
// moderator's (MT).
func seedMCPLeakTokens(c *catalogtest.Catalog) map[revisionViewer]string {
	t, ok := c.T().(*testing.T)
	require.True(t, ok, "the leak fixture is seeded by a test")
	bootstrap := &command.BootstrapModerators{Promoter: mysql.New(c.DB()), Now: now}
	_, err := bootstrap.Execute(t.Context(), []string{mcpLeakModeratorID})
	require.NoError(t, err)
	w := revisionWorldOn(t, c.DB(), nil)

	return map[revisionViewer]string{
		viewerAuthorToken:    w.apiToken(t, w.auth0(t, mcpLeakContributorID)),
		viewerModeratorToken: w.apiToken(t, w.auth0(t, mcpLeakModeratorID)),
	}
}
