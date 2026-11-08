package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tools the tests call most.
const (
	toolGetPiece         = "get_piece"
	toolGetDraft         = "get_draft"
	toolSearchPieces     = "search_pieces"
	toolCreateDraft      = "create_draft"
	toolUpdateDraft      = "update_draft"
	toolDeleteDraft      = "delete_draft"
	toolGetEditable      = "get_editable"
	toolGetPending       = "get_pending"
	toolWithdrawRevision = "withdraw_revision"
)

const (
	slugArgument     = "slug"
	codeField        = "code"
	messageField     = "message"
	invalidDraftID   = "invalid argument: draft_id"
	kettleArguments  = `{"slug":"the-copper-kettle"}`
	draft51Arguments = `{"draft_id":51}`
	piecesPath       = "/v1/pieces"
)

const (
	mcpPath           = "/v1/mcp"
	wireMediaType     = "application/json"
	acceptBoth        = "application/json, text/event-stream"
	protocolHeader    = "Mcp-Protocol-Version"
	handshakeProtocol = "2025-06-18"
	sessionless       = "2026-07-28"
	testAuthorization = "Bearer user_api|morwen-secret"
)

var embeddedToolset = sync.OnceValues(func() (*Toolset, error) { return NewToolset(openapi.Spec) })

// testToolset is the toolset of the embedded spec.
func testToolset(t *testing.T) *Toolset {
	t.Helper()

	tools, err := embeddedToolset()
	require.NoError(t, err)
	return tools
}

// innerRequest is a REST request a tool call made, as the inner router saw it.
type innerRequest struct {
	method   string
	template string
	// escapedPath and query are the request's path and query string as sent.
	escapedPath string
	query       string
	request     *http.Request
	body        string
}

// fakeAPI stands in for the API's router: it routes every operation of the spec, and a few paths tools must
// never reach, as the router does, recording each request and answering it with respond.
type fakeAPI struct {
	mux     *http.ServeMux
	mu      sync.Mutex
	served  []innerRequest
	respond http.HandlerFunc
}

func newFakeAPI(t *testing.T, respond http.HandlerFunc) *fakeAPI {
	t.Helper()

	s, err := parseSpec(openapi.Spec)
	require.NoError(t, err)

	api := &fakeAPI{mux: http.NewServeMux(), respond: respond}
	for _, op := range s.operations {
		pattern := op.method + " " + op.path
		api.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			api.mu.Lock()
			api.served = append(api.served, innerRequest{
				method: r.Method, template: op.path, escapedPath: r.URL.EscapedPath(), query: r.URL.RawQuery,
				request: r, body: string(body),
			})
			api.mu.Unlock()
			api.respond(w, r)
		})
	}
	return api
}

func (api *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	api.mux.ServeHTTP(w, r)
}

// lookup is the router's lookup, modelled: a request whose path is not canonical, or that no route matches with
// its own method, has no route.
func (api *fakeAPI) lookup(r *http.Request) (string, bool) {
	escaped := r.URL.EscapedPath()
	if path.Clean(escaped) != escaped {
		return "", false
	}
	_, pattern := api.mux.Handler(r)
	method, template, ok := strings.Cut(pattern, " ")
	if !ok || method != r.Method {
		return "", false
	}
	return template, true
}

func (api *fakeAPI) requests() []innerRequest {
	api.mu.Lock()
	defer api.mu.Unlock()
	return append([]innerRequest(nil), api.served...)
}

// handler is the MCP handler over api.
func (api *fakeAPI) handler(t *testing.T) http.Handler {
	t.Helper()
	return NewHandler(testToolset(t), api, api.lookup)
}

// respondJSON answers every request with status and body.
func respondJSON(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", wireMediaType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body + "\n"))
	}
}

// mcpRequest is a POST /v1/mcp request with a JSON-RPC message, the headers Streamable HTTP needs and the caller's
// API token, under the 2025-06-18 protocol.
func mcpRequest(t *testing.T, message string) *http.Request {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, mcpPath, strings.NewReader(message))
	req.Header.Set("Content-Type", wireMediaType)
	req.Header.Set("Accept", acceptBoth)
	req.Header.Set("Authorization", testAuthorization)
	req.Header.Set(protocolHeader, handshakeProtocol)
	return req
}

// toolCallMessage is the JSON-RPC message calling tool with the JSON arguments.
func toolCallMessage(tool, arguments string) string {
	return `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + arguments +
		`}}`
}

// toolResult is a tools/call result.
type toolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

// rpcResponse is a JSON-RPC response of the MCP server.
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// callTool calls tool with the JSON arguments through handler, checking the exchange against the spec, and
// returns the tool result.
func callTool(t *testing.T, handler http.Handler, tool, arguments string) toolResult {
	t.Helper()

	rec := openapitest.New(t).Serve(t, handler, mcpRequest(t, toolCallMessage(tool, arguments)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response rpcResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Nil(t, response.Error, "a JSON-RPC error: %s", rec.Body.String())

	var result toolResult
	require.NoError(t, json.Unmarshal(response.Result, &result))
	return result
}

// errorText decodes the text of a tool error, which must be its only content, with no structured content.
func errorText(t *testing.T, result toolResult) map[string]any {
	t.Helper()

	require.True(t, result.IsError)
	assert.Empty(t, result.StructuredContent, "a tool error has no structured content")
	require.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	var text map[string]any
	require.NoError(t, json.Unmarshal([]byte(result.Content[0].Text), &text))
	return text
}

// invalidRequest is the text of a tool error refusing arguments with message.
func invalidRequest(message string) map[string]any {
	return map[string]any{statusField: float64(http.StatusBadRequest), codeField: "invalid_request", messageField: message}
}
