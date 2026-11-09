package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// leakmark is in argument values the server must never echo or log.
const leakmark = "leakmark-argument"

// markedPublicID is a public ID, which leakmark is not, holding the marker too.
const markedPublicID = "leakmarkleakmark"

// TestMCP_ArgumentValidation checks that arguments the tool's input schema refuses are a tool error 400
// invalid_request naming the argument, never its value, and reach no operation.
func TestMCP_ArgumentValidation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, tool, arguments, message string
	}{
		{"missing required", toolGetPiece, `{}`, "missing argument: slug"},
		{"wrong type", toolGetDraft, `{"draft_id":"` + leakmark + `"}`, invalidDraftID},
		{"not an integer", toolGetDraft, `{"draft_id":1.5}`, invalidDraftID},
		{"out of range", toolGetDraft, `{"draft_id":0}`, invalidDraftID},
		{"pattern mismatch", toolGetPiece, `{"slug":"Not-A-Slug-` + leakmark + `"}`, "invalid argument: slug"},
		{"query out of range", toolSearchPieces, `{"limit":1000}`, "invalid argument: limit"},
		{"array element pattern mismatch", toolSearchPieces, `{"term":["war","` + leakmark + ` x"]}`,
			"invalid argument: term"},
		{"enum mismatch", toolSearchPieces, `{"kind":["` + leakmark + `"]}`, "invalid argument: kind"},
		{"null optional", toolSearchPieces, `{"q":null}`, "invalid argument: q"},
		{"extra property", toolGetPiece, `{"slug":"the-copper-kettle","` + leakmark + `":1}`,
			unexpectedArgumentMessage},
		{"arguments not an object", toolGetPiece, `["` + leakmark + `"]`, "the arguments must be a JSON object"},
		{"body not an object", toolCreateDraft, `{"body":"` + leakmark + `"}`,
			"invalid argument: body: must be a JSON object"},
		{"missing body", toolCreateDraft, `{}`, "missing argument: body"},
		{"missing entity type", toolGetEditable, kettleArguments, "missing argument: entity_type"},
		{"unknown entity type", toolGetEditable, `{"entity_type":"` + leakmark + `","slug":"the-copper-kettle"}`,
			"invalid argument: entity_type"},
		{"collection by slug", toolGetEditable, `{"entity_type":"collection","slug":"the-copper-kettle"}`,
			unexpectedArgumentMessage},
		{"collection without its ID", toolGetPending, `{"entity_type":"collection"}`,
			"missing argument: collection_id"},
		{"piece by collection ID", toolGetPending, `{"entity_type":"piece","collection_id":12}`,
			unexpectedArgumentMessage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			api := newFakeAPI(t, respondJSON(http.StatusOK, `{}`))
			result := callTool(t, api.handler(t), tc.tool, tc.arguments)

			assert.Equal(t, invalidRequest(tc.message), errorText(t, result))
			assert.NotContains(t, result.Content[0].Text, leakmark, "the error echoes a value")
			assert.Empty(t, api.requests(), "refused arguments reached an operation")
		})
	}
}

// TestMCP_ToolCallsMakeTheirOperationsRequests checks the REST request each kind of argument makes: path
// parameters percent-encoded into the operation's own path, query parameters (arrays once per element), the body
// as sent, and for a tool with an operation per entity type the operation entity_type chooses.
func TestMCP_ToolCallsMakeTheirOperationsRequests(t *testing.T) {
	t.Parallel()

	const document = `{"entity_type":"event","entity":null,"base_revision_id":null,` +
		`"document":{"name":"Midwinter Revel","kind":"event","description_md":null},"summary":"New event."}`
	for _, tc := range []struct {
		name, tool, arguments         string
		method, path, query, body     string
		wantContentType, wantTemplate string
	}{
		{name: "path parameter", tool: toolGetPiece, arguments: kettleArguments,
			method: http.MethodGet, path: "/v1/pieces/the-copper-kettle", wantTemplate: "/v1/pieces/{slug}"},
		{name: "integer path parameter", tool: toolGetDraft, arguments: draft51Arguments,
			method: http.MethodGet, path: "/v1/drafts/51", wantTemplate: "/v1/drafts/{draft_id}"},
		{name: "query parameters", tool: toolSearchPieces,
			arguments: `{"term":["war","swords"],"stub":false,"limit":5,"q":"wild heron"}`,
			method:    http.MethodGet, path: piecesPath, query: "limit=5&q=wild+heron&stub=false&term=war&term=swords",
			wantTemplate: piecesPath},
		{name: "chosen by entity type", tool: toolGetEditable, arguments: `{"entity_type":"collection","collection_id":12}`,
			method: http.MethodGet, path: "/v1/collections/12/editable",
			wantTemplate: "/v1/collections/{collection_id}/editable"},
		{name: "body", tool: toolCreateDraft, arguments: `{"body":` + document + `}`, method: http.MethodPost,
			path: "/v1/drafts", body: document, wantContentType: wireMediaType, wantTemplate: "/v1/drafts"},
		{name: "no body", tool: toolWithdrawRevision, arguments: `{"revision_id":51}`, method: http.MethodPost,
			path: "/v1/revisions/51/withdraw", wantTemplate: "/v1/revisions/{revision_id}/withdraw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			api := newFakeAPI(t, respondJSON(http.StatusOK, `{"ok":true}`))
			result := callTool(t, api.handler(t), tc.tool, tc.arguments)
			require.False(t, result.IsError, result.Content)

			require.Len(t, api.requests(), 1)
			got := api.requests()[0]
			assert.Equal(t, tc.method, got.method)
			assert.Equal(t, tc.wantTemplate, got.template)
			assert.Equal(t, tc.path, got.escapedPath)
			assert.Equal(t, tc.query, got.query)
			assert.Equal(t, tc.body, got.body)
			assert.Equal(t, tc.wantContentType, got.request.Header.Get("Content-Type"))
			assert.Equal(t, testAuthorization, got.request.Header.Get("Authorization"), "the caller's own credentials")
		})
	}
}

// TestMCP_DispatchPinsRoute builds requests from argument values the input schemas refuse, bypassing them, and
// checks that each either reaches the tool's own operation, with the value inside its own path segment, or is
// refused with 400 invalid_request: no value reaches another operation, and no request is redirected.
func TestMCP_DispatchPinsRoute(t *testing.T) {
	t.Parallel()

	tools := testToolset(t)
	for _, tc := range []struct {
		tool, argument, value string
		// reaches is whether the request reaches the tool's operation; otherwise it is refused.
		reaches bool
	}{
		{tool: toolGetPiece, argument: slugArgument, value: "../moderation/queue", reaches: true},
		{tool: toolGetPiece, argument: slugArgument, value: "x%2Feditable", reaches: true},
		{tool: toolGetPiece, argument: slugArgument, value: "x/../../tokens", reaches: true},
		{tool: toolGetPiece, argument: slugArgument, value: "a?include_blocked=true", reaches: true},
		{tool: toolGetPiece, argument: slugArgument, value: "x/history", reaches: true},
		{tool: toolGetPiece, argument: slugArgument, value: "..", reaches: false},
		{tool: toolGetPiece, argument: slugArgument, value: ".", reaches: false},
		{tool: toolGetPiece, argument: slugArgument, value: "", reaches: false},
		{tool: toolGetDraft, argument: "draft_id", value: "1/x", reaches: true},
		{tool: "get_my_repertoire_entry", argument: slugArgument, value: "../../revisions", reaches: true},
		{tool: "list_public_repertoire", argument: "public_id", value: "../../me", reaches: true},
	} {
		t.Run(tc.tool+" "+tc.value, func(t *testing.T) {
			t.Parallel()

			api := newFakeAPI(t, respondJSON(http.StatusNotFound, `{"code":"not_found","message":"not found"}`))
			tl := toolNamed(t, tools, tc.tool)
			op := tl.operations[""]
			raw, err := json.Marshal(tc.value)
			require.NoError(t, err)
			args := arguments{raw: map[string]json.RawMessage{tc.argument: raw}, values: map[string]any{tc.argument: tc.value}}

			outer := mcpRequest(t, toolsListMessage)
			status, route, result := dispatcher{outer: outer, inner: api, lookup: api.lookup}.serve(op, args)

			requests := api.requests()
			if !tc.reaches {
				assert.Equal(t, http.StatusBadRequest, status)
				assert.Empty(t, route)
				assert.Empty(t, requests)
				text := errorText(t, toolResultOf(t, result))
				assert.Equal(t, invalidRequest(notAddressedMessage), text)
				return
			}

			require.Len(t, requests, 1)
			assert.Equal(t, op.path, requests[0].template, "the request reached another operation")
			assert.Equal(t, op.path, route)
			assert.Equal(t, tc.value, requests[0].request.PathValue(tc.argument), "the value left its segment")
			assert.Empty(t, requests[0].query, "the value reached the query string")
			assert.Equal(t, http.StatusNotFound, status)
		})
	}
}

// TestMCP_ToolCallResults checks how REST responses become tool results: a 2xx body is the structured content
// and the text; a 2xx without a body is its status as text; any other status is a tool error with text content
// only, holding the REST error body and its status, and for a 429 the seconds to wait but nothing naming the
// limit.
func TestMCP_ToolCallResults(t *testing.T) {
	t.Parallel()

	const problems = `{"code":"invalid_document","message":"the document is invalid",` +
		`"problems":[{"path":"/title","problem":"blank"}]}`
	for _, tc := range []struct {
		name     string
		respond  http.HandlerFunc
		tool     string
		args     string
		wantBody string
		wantText map[string]any
	}{
		{name: "200", respond: respondJSON(http.StatusOK, `{"slug":"the-copper-kettle","title":"The Copper Kettle"}`),
			tool: toolGetPiece, args: kettleArguments,
			wantBody: `{"slug":"the-copper-kettle","title":"The Copper Kettle"}`},
		{name: "201", respond: respondJSON(http.StatusCreated, draft51Arguments), tool: toolCreateDraft,
			args: `{"body":{}}`, wantBody: draft51Arguments},
		{name: "204", respond: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			tool: toolDeleteDraft, args: draft51Arguments, wantBody: `{"status":204}`},
		{name: "404", respond: respondJSON(http.StatusNotFound, `{"code":"not_found","message":"piece not found"}`),
			tool: toolGetPiece, args: kettleArguments,
			wantText: map[string]any{statusField: float64(404), codeField: "not_found", messageField: "piece not found"}},
		{name: "422 with problems", respond: respondJSON(http.StatusUnprocessableEntity, problems),
			tool: toolCreateDraft, args: `{"body":{}}`, wantText: map[string]any{
				statusField: float64(422), codeField: "invalid_document", messageField: "the document is invalid",
				"problems": []any{map[string]any{"path": "/title", "problem": "blank"}},
			}},
		{name: "429", respond: func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Retry-After", "7")
			respondJSON(http.StatusTooManyRequests, `{"code":"rate_limited","message":"too many requests"}`)(w, r)
		}, tool: toolUpdateDraft, args: `{"draft_id":51,"body":{}}`, wantText: map[string]any{
			statusField: float64(429), codeField: "rate_limited", messageField: "too many requests",
			"retry_after_seconds": float64(7),
		}},
		{name: "503", respond: respondJSON(http.StatusServiceUnavailable, `{"code":"unavailable","message":"try again"}`),
			tool: "list_my_revisions", args: `{}`,
			wantText: map[string]any{statusField: float64(503), codeField: "unavailable", messageField: "try again"}},
		{name: "error without a body", respond: func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}, tool: toolGetPiece, args: kettleArguments, wantText: map[string]any{
			statusField: float64(500), codeField: "internal", messageField: "the operation answered without an error body",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			api := newFakeAPI(t, tc.respond)
			result := callTool(t, api.handler(t), tc.tool, tc.args)

			if tc.wantText != nil {
				assert.Equal(t, tc.wantText, errorText(t, result))
				assert.NotContains(t, result.Content[0].Text, "tier")
				assert.NotContains(t, result.Content[0].Text, "_user")
				return
			}
			assert.False(t, result.IsError)
			require.Len(t, result.Content, 1)
			assert.JSONEq(t, tc.wantBody, result.Content[0].Text)
			if tc.wantBody == `{"status":204}` {
				assert.Empty(t, result.StructuredContent)
			} else {
				assert.JSONEq(t, tc.wantBody, string(result.StructuredContent))
			}
		})
	}
}

// TestMCP_ToolDispatchKeepsRequestContext checks that the REST request a tool call makes has the MCP request's
// context: its deadline, its logger (and so its request ID) and everything middleware stored in it, and the
// caller's credentials and peer.
func TestMCP_ToolDispatchKeepsRequestContext(t *testing.T) {
	t.Parallel()

	type recordKey struct{}
	record := &struct{ userID string }{userID: "auth0|morwen"}
	logger := slog.New(slog.DiscardHandler).With("request_id", "a-request-id")
	// Far in the future, so that no run, however loaded, reaches it.
	deadline := time.Now().Add(time.Hour)

	api := newFakeAPI(t, respondJSON(http.StatusOK, `{}`))

	withDeadline, cancel := context.WithDeadline(t.Context(), deadline)
	t.Cleanup(cancel)
	outer := domain.ContextWithLogger(context.WithValue(withDeadline, recordKey{}, record), logger)
	req := mcpRequest(t, toolCallMessage(toolGetPiece, kettleArguments)).WithContext(outer)
	req.RemoteAddr = "198.51.100.7:4711"
	rec := openapitest.New(t).Serve(t, api.handler(t), req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, api.requests(), 1, "the tool call reached its operation")
	inner := api.requests()[0].request.Context()
	got, ok := inner.Deadline()
	assert.True(t, ok)
	assert.Equal(t, deadline, got)
	assert.Same(t, logger, domain.LoggerFromContext(inner))
	assert.Same(t, record, inner.Value(recordKey{}))
	assert.Equal(t, "198.51.100.7:4711", api.requests()[0].request.RemoteAddr)
}

// mcpLogLine is the line a tool call logs.
type mcpLogLine struct {
	Msg       string `json:"msg"`
	Tool      string `json:"tool"`
	Status    int    `json:"status"`
	ToolRoute string `json:"tool_route"`
	RequestID string `json:"request_id"`
}

// callLogged calls tool with arguments through handler, under a logger of its own, and returns the call's log.
func callLogged(t *testing.T, handler http.Handler, tool, arguments string) string {
	t.Helper()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil)).With("request_id", "a-request-id")
	req := mcpRequest(t, toolCallMessage(tool, arguments))
	req = req.WithContext(domain.ContextWithLogger(req.Context(), logger))
	rec := openapitest.New(t).Serve(t, handler, req)
	require.Equal(t, http.StatusOK, rec.Code)

	return logs.String()
}

// TestMCP_NoArgumentLogging checks that every tool's call logs one line, through the MCP request's logger, naming
// the tool, the route it reached and the status, and never an argument value or a result: each tool is called
// with leakmark in every argument that can hold it (markedArguments) and in the body, and answered with a result
// holding it too.
func TestMCP_NoArgumentLogging(t *testing.T) {
	t.Parallel()

	api := newFakeAPI(t, respondJSON(http.StatusOK, `{"title":"`+leakmark+`-result"}`))
	handler := api.handler(t)
	tools := testToolset(t)
	for _, tl := range tools.tools {
		for key, op := range tl.operations {
			t.Run(tl.Name+" "+key, func(t *testing.T) {
				t.Parallel()

				logs := callLogged(t, handler, tl.Name, markedArguments(t, op, key))
				assert.NotContains(t, logs, leakmark)
				assert.NotContains(t, logs, markedPublicID)
				var line mcpLogLine
				require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(logs)), &line), logs)
				assert.Equal(t, mcpLogLine{Msg: "mcp tool called", Tool: tl.Name, Status: http.StatusOK,
					ToolRoute: op.path, RequestID: "a-request-id"}, line)
			})
		}
	}

	t.Run("refused", func(t *testing.T) {
		t.Parallel()

		for _, arguments := range []string{`{"q":"` + leakmark + `","limit":1000}`, `{"` + leakmark + `":true}`} {
			logs := callLogged(t, handler, toolSearchPieces, arguments)
			assert.NotContains(t, logs, leakmark)
			var line mcpLogLine
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(logs)), &line), logs)
			assert.Equal(t, mcpLogLine{Msg: "mcp tool called", Tool: toolSearchPieces, Status: http.StatusBadRequest,
				RequestID: "a-request-id"}, line, "a refused call reaches no route")
		}
	})
}

// markedArguments are arguments of a call of op, chosen by entity type key (empty for a tool of one operation),
// that pass its validation: each parameter whose schema accepts a string holding leakmark gets one, each other
// required parameter a value its schema accepts, and the body, if op takes one, an object holding leakmark.
func markedArguments(t *testing.T, op operation, key string) string {
	t.Helper()

	args := map[string]any{}
	if key != "" {
		args[entityTypeArgument] = key
	}
	for _, p := range op.parameters {
		value, ok := acceptedValue(op, p)
		require.True(t, ok || !p.required, "no value is accepted for the required parameter %s", p.name)
		if ok {
			args[p.name] = value
		}
	}
	if op.hasBody {
		args[bodyArgument] = map[string]any{leakmark: leakmark + "-body"}
	}
	data, err := json.Marshal(args)
	require.NoError(t, err)

	return string(data)
}

// acceptedValue is the first value of p's candidates its validator accepts: strings and arrays holding leakmark
// first, then a positive integer, a boolean and its schema's enum values.
func acceptedValue(op operation, p parameter) (any, bool) {
	candidates := []any{leakmark, markedPublicID, []any{leakmark}, float64(51), true}
	candidates = append(candidates, enumValues(op, p.schema)...)
	for _, c := range candidates {
		if op.validators[p.name].Validate(c) == nil {
			return c, true
		}
	}

	return nil, false
}

// enumValues are the values of schema's enum, or its items', following one reference to op's definitions; a
// value is offered alone and as an array of one.
func enumValues(op operation, schema object) []any {
	if ref, ok := schema["$ref"].(string); ok {
		if def, ok := op.defs[strings.TrimPrefix(ref, "#/$defs/")].(object); ok {
			schema = def
		}
	}
	if items, ok := schema["items"].(object); ok {
		values := enumValues(op, items)
		out := make([]any, 0, len(values))
		for _, v := range values {
			out = append(out, []any{v})
		}

		return out
	}
	values, _ := schema["enum"].([]any)

	return values
}

func toolNamed(t *testing.T, tools *Toolset, name string) tool {
	t.Helper()
	for _, tl := range tools.tools {
		if tl.Name == name {
			return tl
		}
	}
	require.Failf(t, "no tool", "%s", name)
	return tool{}
}

// toolResultOf is result as a client decodes it.
func toolResultOf(t *testing.T, result any) toolResult {
	t.Helper()

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	var decoded toolResult
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	return decoded
}

// TestScalarValue checks how argument values are sent in a path or query string.
func TestScalarValue(t *testing.T) {
	t.Parallel()

	for raw, want := range map[string]string{
		`"a b/c"`: "a b/c", `true`: "true", `false`: "false", `42`: "42", `9007199254740991`: "9007199254740991",
		`1e3`: "1000", `10.0`: "10", `-7`: "-7", `1.5e1`: "15", `-0`: "0", `9223372036854775807`: "9223372036854775807",
		`-9.223372036854775808e18`: "-9223372036854775808", `2500e-2`: "25", `1E2`: "100",
	} {
		got, err := scalarValue("x", json.RawMessage(raw))
		require.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
	for _, raw := range []string{
		`1.5`, `1e-1`, `-2.5e0`, `9223372036854775808`, `1e19`, `1e999999999`, `+1`, `01`, `0x10`, `"unterminated`,
		`null`, `{}`,
	} {
		_, err := scalarValue("x", json.RawMessage(raw))
		assert.Error(t, err, raw)
	}
}

// expiringContext is a request context whose deadline passes when expire is called: a request budget that runs
// out at the moment a test chooses, rather than after a wall-clock wait.
type expiringContext struct {
	context.Context

	once sync.Once
	done chan struct{}
}

func newExpiringContext(parent context.Context) *expiringContext {
	return &expiringContext{Context: parent, done: make(chan struct{})}
}

func (c *expiringContext) expire() {
	c.once.Do(func() { close(c.done) })
}

func (c *expiringContext) Deadline() (time.Time, bool) {
	return time.Now(), true
}

func (c *expiringContext) Done() <-chan struct{} {
	return c.done
}

func (c *expiringContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

// TestMCP_DeadlineDuringCallKeepsResult checks that a tool call whose request budget runs out while its operation
// runs still gets the operation's answer as its result: the operation sees its context end, answers anyway (as a
// write that committed would), and the client is told what it answered rather than getting an empty response.
func TestMCP_DeadlineDuringCallKeepsResult(t *testing.T) {
	t.Parallel()

	ctx := newExpiringContext(t.Context())
	api := newFakeAPI(t, func(w http.ResponseWriter, r *http.Request) {
		ctx.expire()
		<-r.Context().Done()
		// The answer comes well after the budget ran out, as a write retried past it would. The handler must wait for
		// it whatever the delay; the delay only stops an answer racing the end of the budget from hiding a handler
		// that gives up at that end.
		time.Sleep(100 * time.Millisecond)
		respondJSON(http.StatusCreated, draft51Arguments)(w, r)
	})
	req := mcpRequest(t, toolCallMessage(toolCreateDraft, `{"body":{}}`)).WithContext(ctx)
	rec := openapitest.New(t).Serve(t, api.handler(t), req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response rpcResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response), "body=%q", rec.Body.String())
	require.Nil(t, response.Error, rec.Body.String())
	var result toolResult
	require.NoError(t, json.Unmarshal(response.Result, &result))
	assert.False(t, result.IsError)
	require.Len(t, result.Content, 1)
	assert.JSONEq(t, draft51Arguments, result.Content[0].Text)
	assert.JSONEq(t, draft51Arguments, string(result.StructuredContent))
	assert.Len(t, api.requests(), 1)
}

// TestMCP_ExpiredBudgetIs503 checks that a request whose budget has run out before the MCP server runs, as after a
// slow upload, is a JSON 503 unavailable that calls no tool.
func TestMCP_ExpiredBudgetIs503(t *testing.T) {
	t.Parallel()

	ctx := newExpiringContext(t.Context())
	ctx.expire()
	api := newFakeAPI(t, respondJSON(http.StatusCreated, draft51Arguments))
	req := mcpRequest(t, toolCallMessage(toolCreateDraft, `{"body":{}}`)).WithContext(ctx)
	rec := openapitest.New(t).Serve(t, api.handler(t), req)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code, rec.Body.String())
	assert.Equal(t, wireMediaType, rec.Header().Get("Content-Type"))
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body), "body=%q", rec.Body.String())
	assert.Equal(t, "unavailable", body[codeField])
	assert.Empty(t, api.requests(), "a tool was called after the budget ran out")
}

// TestUnencodableResult pins the text of a result that could not be encoded: a 500 internal tool error.
func TestUnencodableResult(t *testing.T) {
	t.Parallel()

	var text map[string]any
	require.NoError(t, json.Unmarshal([]byte(unencodableResult()), &text))
	assert.Equal(t, map[string]any{
		"status": float64(500), "code": "internal", "message": "unable to encode the result",
	}, text)
}
