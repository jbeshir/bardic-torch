package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const toolsListMessage = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

// TestMCP_PreChecks checks, through the contract helper, the answers to requests Streamable HTTP refuses: our
// own JSON errors for the media type, the Accept header and the body size, and the SDK's plain-text and JSON-RPC
// 400s. None reaches a REST operation.
func TestMCP_PreChecks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		request     func(*http.Request)
		body        string
		wantStatus  int
		wantType    string
		wantBody    string
		wantContain string
	}{
		{
			name:       "not JSON",
			request:    func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
			wantStatus: http.StatusUnsupportedMediaType, wantType: wireMediaType,
			wantBody: `{"code":"unsupported_media_type","message":"request body must be sent as application/json"}`,
		},
		{
			name:       "no Content-Type",
			request:    func(r *http.Request) { r.Header.Del("Content-Type") },
			wantStatus: http.StatusUnsupportedMediaType, wantType: wireMediaType,
			wantBody: `{"code":"unsupported_media_type","message":"request body must be sent as application/json"}`,
		},
		{
			name:       "Accept without the event stream",
			request:    func(r *http.Request) { r.Header.Set("Accept", wireMediaType) },
			wantStatus: http.StatusNotAcceptable, wantType: wireMediaType,
			wantBody: `{"code":"not_acceptable",` +
				`"message":"the Accept header must list both application/json and text/event-stream"}`,
		},
		{
			name:       "no Accept",
			request:    func(r *http.Request) { r.Header.Del("Accept") },
			wantStatus: http.StatusNotAcceptable, wantType: wireMediaType,
			wantBody: `{"code":"not_acceptable",` +
				`"message":"the Accept header must list both application/json and text/event-stream"}`,
		},
		{
			name:       "body too large",
			body:       `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"x":"` + strings.Repeat("a", MaxBodyBytes) + `"}}`,
			wantStatus: http.StatusRequestEntityTooLarge, wantType: wireMediaType,
			wantBody: `{"code":"request_too_large","message":"request body is too large"}`,
		},
		{
			name:       "unsupported protocol version",
			request:    func(r *http.Request) { r.Header.Set(protocolHeader, "1999-01-01") },
			wantStatus: http.StatusBadRequest, wantType: "text/plain; charset=utf-8",
			wantContain: "Unsupported protocol version",
		},
		{
			name: "sessionless message without its protocol header",
			request: func(r *http.Request) {
				r.Header.Del(protocolHeader)
			},
			body:       sessionlessMessage("tools/list", `{}`),
			wantStatus: http.StatusBadRequest, wantType: wireMediaType,
			wantContain: `"error":`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			api := newFakeAPI(t, respondJSON(http.StatusOK, `{}`))
			body := toolsListMessage
			if tc.body != "" {
				body = tc.body
			}
			req := mcpRequest(t, body)
			if tc.request != nil {
				tc.request(req)
			}
			rec := openapitest.New(t).ServeResponseOnly(t, api.handler(t), req)

			assert.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			assert.Equal(t, tc.wantType, rec.Header().Get("Content-Type"))
			if tc.wantBody != "" {
				assert.JSONEq(t, tc.wantBody, rec.Body.String())
			}
			assert.Contains(t, rec.Body.String(), tc.wantContain)
			assert.Empty(t, api.requests(), "a refused request reached an operation")
		})
	}
}

// TestMCP_LocalhostProtectionDisabled checks that a request arriving over a loopback connection with a public
// Host, as every request does behind dokku's nginx, is served: the SDK's DNS rebinding protection would refuse it.
func TestMCP_LocalhostProtectionDisabled(t *testing.T) {
	t.Parallel()

	api := newFakeAPI(t, respondJSON(http.StatusOK, `{}`))
	server := httptest.NewServer(api.handler(t))
	t.Cleanup(server.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+mcpPath,
		strings.NewReader(toolsListMessage))
	require.NoError(t, err)
	req.Host = "api.bardictorch.org"
	req.Header.Set("Content-Type", wireMediaType)
	req.Header.Set("Accept", acceptBoth)
	req.Header.Set(protocolHeader, handshakeProtocol)
	resp, err := server.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// sessionlessMessage is a JSON-RPC request of method under the sessionless protocol, which carries the protocol
// version and client in each request's _meta. params is a JSON object without _meta.
func sessionlessMessage(method, params string) string {
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"` + sessionless + `",` +
		`"io.modelcontextprotocol/clientInfo":{"name":"test-client","version":"1.0"},` +
		`"io.modelcontextprotocol/clientCapabilities":{}}`
	params = strings.TrimPrefix(params, "{")
	if params != "}" {
		meta += ","
	}
	return `{"jsonrpc":"2.0","id":3,"method":"` + method + `","params":{` + meta + params + `}`
}

// listedTool is a tool as tools/list lists it.
type listedTool struct {
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema"`
	Annotations  struct {
		ReadOnlyHint    bool  `json:"readOnlyHint"`
		DestructiveHint *bool `json:"destructiveHint"`
		IdempotentHint  bool  `json:"idempotentHint"`
	} `json:"annotations"`
}

// TestMCP_ToolsList lists the tools under both protocol versions: after the 2025-06-18 handshake (initialize,
// then the initialized notification, each its own stateless request) and in one sessionless 2026-07-28 request.
// The list is the tool table, by name, with no output schemas.
func TestMCP_ToolsList(t *testing.T) {
	t.Parallel()

	tools := testToolset(t).Tools()
	inputs := map[string]*jsonschema.Schema{}
	for _, tl := range testToolset(t).tools {
		inputs[tl.Name] = tl.input
	}
	slices.SortFunc(tools, func(a, b Tool) int { return strings.Compare(a.Name, b.Name) })
	for _, tc := range []struct {
		name    string
		prepare func(t *testing.T, handler http.Handler)
		request func(t *testing.T) *http.Request
	}{
		{
			name: handshakeProtocol,
			prepare: func(t *testing.T, handler http.Handler) {
				t.Helper()
				contract := openapitest.New(t)

				rec := contract.Serve(t, handler, mcpRequest(t, `{"jsonrpc":"2.0","id":1,"method":"initialize",`+
					`"params":{"protocolVersion":"`+handshakeProtocol+`","capabilities":{},`+
					`"clientInfo":{"name":"test-client","version":"1.0"}}}`))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				var response rpcResponse
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
				var initialized struct {
					ProtocolVersion string                     `json:"protocolVersion"`
					Capabilities    map[string]json.RawMessage `json:"capabilities"`
					ServerInfo      struct {
						Name    string `json:"name"`
						Version string `json:"version"`
					} `json:"serverInfo"`
				}
				require.NoError(t, json.Unmarshal(response.Result, &initialized))
				assert.Equal(t, handshakeProtocol, initialized.ProtocolVersion)
				assert.Equal(t, serverName, initialized.ServerInfo.Name)
				assert.Equal(t, testToolset(t).version, initialized.ServerInfo.Version)
				assert.NotEmpty(t, initialized.ServerInfo.Version)
				assert.ElementsMatch(t, []string{"tools"}, mapKeys(initialized.Capabilities), "tools only")

				rec = contract.Serve(t, handler, mcpRequest(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`))
				require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
				assert.Empty(t, rec.Body.String())
			},
			request: func(t *testing.T) *http.Request {
				t.Helper()
				return mcpRequest(t, toolsListMessage)
			},
		},
		{
			name:    sessionless,
			prepare: func(*testing.T, http.Handler) {},
			request: func(t *testing.T) *http.Request {
				t.Helper()
				req := mcpRequest(t, sessionlessMessage("tools/list", `{}`))
				req.Header.Set(protocolHeader, sessionless)
				req.Header.Set("Mcp-Method", "tools/list")
				return req
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			api := newFakeAPI(t, respondJSON(http.StatusOK, `{}`))
			handler := api.handler(t)
			tc.prepare(t, handler)

			rec := openapitest.New(t).Serve(t, handler, tc.request(t))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			assert.Equal(t, wireMediaType, rec.Header().Get("Content-Type"))
			assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			var response rpcResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			require.Nil(t, response.Error, rec.Body.String())
			var listed struct {
				Tools []listedTool `json:"tools"`
			}
			require.NoError(t, json.Unmarshal(response.Result, &listed))

			require.Len(t, listed.Tools, len(tools))
			for i, tl := range tools {
				got := listed.Tools[i]
				assert.Equal(t, tl.Name, got.Name)
				assert.Equal(t, tl.Description, got.Description)
				assert.Empty(t, got.OutputSchema, "%s declares no output schema", tl.Name)
				assert.Equal(t, tl.Annotations.ReadOnly, got.Annotations.ReadOnlyHint, tl.Name)
				assert.Equal(t, tl.Annotations.Idempotent, got.Annotations.IdempotentHint, tl.Name)
				if tl.Annotations.ReadOnly {
					assert.Nil(t, got.Annotations.DestructiveHint, tl.Name)
				} else if assert.NotNil(t, got.Annotations.DestructiveHint, tl.Name) {
					assert.Equal(t, tl.Annotations.Destructive, *got.Annotations.DestructiveHint, tl.Name)
				}
				wantInput, err := json.Marshal(inputs[tl.Name])
				require.NoError(t, err)
				assert.JSONEq(t, string(wantInput), string(got.InputSchema), tl.Name)
			}
			assert.Empty(t, api.requests(), "listing the tools made a REST request")
		})
	}
}

// TestMCP_SessionlessToolCall calls a tool in one sessionless 2026-07-28 request.
func TestMCP_SessionlessToolCall(t *testing.T) {
	t.Parallel()

	api := newFakeAPI(t, respondJSON(http.StatusOK, kettleArguments))
	req := mcpRequest(t, sessionlessMessage("tools/call", `{"name":"get_piece","arguments":{"slug":"the-copper-kettle"}}`))
	req.Header.Set(protocolHeader, sessionless)
	req.Header.Set("Mcp-Method", "tools/call")
	req.Header.Set("Mcp-Name", toolGetPiece)
	rec := openapitest.New(t).Serve(t, api.handler(t), req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response rpcResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	var result toolResult
	require.NoError(t, json.Unmarshal(response.Result, &result))
	assert.False(t, result.IsError)
	assert.JSONEq(t, kettleArguments, string(result.StructuredContent))
	require.Len(t, api.requests(), 1)
	assert.Equal(t, "/v1/pieces/the-copper-kettle", api.requests()[0].escapedPath)
}

// TestMCP_UnknownToolIsAJSONRPCError checks that calling a tool the server does not have is a JSON-RPC error,
// not a tool result, and reaches no operation.
func TestMCP_UnknownToolIsAJSONRPCError(t *testing.T) {
	t.Parallel()

	api := newFakeAPI(t, respondJSON(http.StatusOK, `{}`))
	rec := openapitest.New(t).Serve(t, api.handler(t), mcpRequest(t, toolCallMessage("approve_revision", `{}`)))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response rpcResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.NotNil(t, response.Error, rec.Body.String())
	assert.Empty(t, api.requests())
}

func mapKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

// TestMCP_RefusesBatches checks that a JSON-RPC batch is refused with 400 invalid_request whatever the protocol
// header says, before any tool is called: without a header the MCP SDK would read it as 2025-03-26, which allows
// batches, and so would a 2025-03-26 header if the server negotiated that version.
func TestMCP_RefusesBatches(t *testing.T) {
	t.Parallel()

	call := toolCallMessage(toolGetPiece, kettleArguments)
	for _, tc := range []struct{ name, version string }{
		{name: "no protocol header"},
		{name: batchingProtocol, version: batchingProtocol},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			api := newFakeAPI(t, respondJSON(http.StatusOK, kettleArguments))
			req := mcpRequest(t, "\n ["+call+","+call+"]")
			req.Header.Del(protocolHeader)
			if tc.version != "" {
				req.Header.Set(protocolHeader, tc.version)
			}
			// The request is a batch, which the spec's request body does not allow, so only the response is checked.
			rec := openapitest.New(t).ServeResponseOnly(t, api.handler(t), req)

			assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
			assert.Equal(t, wireMediaType, rec.Header().Get("Content-Type"))
			assert.JSONEq(t, `{"code":"invalid_request","message":"JSON-RPC batches are not supported"}`,
				rec.Body.String())
			assert.Empty(t, api.requests(), "a batched call reached an operation")
		})
	}
}

// TestMCP_ProtocolVersions checks that the server serves exactly the protocol versions it documents: an older
// version's header is a plain-text 400 naming exactly 2026-07-28, 2025-11-25 and 2025-06-18, and the handshake
// versions among those serve tools/list.
func TestMCP_ProtocolVersions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		version    string
		wantStatus int
	}{
		{version: "2024-11-05", wantStatus: http.StatusBadRequest},
		{version: batchingProtocol, wantStatus: http.StatusBadRequest},
		{version: "2025-06-18", wantStatus: http.StatusOK},
		{version: "2025-11-25", wantStatus: http.StatusOK},
	} {
		t.Run(tc.version, func(t *testing.T) {
			t.Parallel()

			api := newFakeAPI(t, respondJSON(http.StatusOK, `{}`))
			req := mcpRequest(t, toolsListMessage)
			req.Header.Set(protocolHeader, tc.version)
			rec := openapitest.New(t).Serve(t, api.handler(t), req)

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			if tc.wantStatus == http.StatusBadRequest {
				assert.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
				assert.Equal(t, "Bad Request: Unsupported protocol version "+
					"(supported versions: 2026-07-28,2025-11-25,2025-06-18)\n", rec.Body.String())
			}
			assert.Empty(t, api.requests())
		})
	}
}

// TestMCP_UnknownMethod20260728 checks, against the spec, the answers to well-formed sessionless 2026-07-28
// requests for methods other than the tools': a method MCP does not define is HTTP 404 with a JSON-RPC
// method-not-found error, as that protocol requires, and resources/list, which MCP defines, is an empty list.
func TestMCP_UnknownMethod20260728(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		method     string
		wantStatus int
	}{
		{method: "bardic/unknown", wantStatus: http.StatusNotFound},
		{method: "resources/list", wantStatus: http.StatusOK},
	} {
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()

			api := newFakeAPI(t, respondJSON(http.StatusOK, `{}`))
			req := mcpRequest(t, sessionlessMessage(tc.method, `{}`))
			req.Header.Set(protocolHeader, sessionless)
			req.Header.Set("Mcp-Method", tc.method)
			rec := openapitest.New(t).Serve(t, api.handler(t), req)

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			assert.Equal(t, wireMediaType, rec.Header().Get("Content-Type"))
			var response rpcResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			if tc.wantStatus == http.StatusNotFound {
				require.NotNil(t, response.Error, rec.Body.String())
				assert.Equal(t, -32601, response.Error.Code, "method not found")
			} else {
				require.Nil(t, response.Error, rec.Body.String())
				var listed struct {
					Resources []json.RawMessage `json:"resources"`
				}
				require.NoError(t, json.Unmarshal(response.Result, &listed))
				assert.Empty(t, listed.Resources)
			}
			assert.Empty(t, api.requests())
		})
	}
}
