package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/mcp"
	"github.com/jbeshir/bardic-torch/openapi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The MCP requests the app tests send: tool calls under the 2025-06-18 protocol, which the stateless server
// answers without a handshake, through the production handler.

const (
	mcpPath           = "/v1/mcp"
	mcpAccept         = "application/json, text/event-stream"
	mcpProtocolHeader = "Mcp-Protocol-Version"
	mcpProtocol       = "2025-06-18"
	// bodyArgument is the argument that carries a tool's request body.
	bodyArgument = "body"
	// entityTypeArgument chooses the operation of a tool with one per entity type.
	entityTypeArgument = "entity_type"
)

// toolOperation is a tool and one of its operations.
type toolOperation struct {
	tool mcp.Tool
	op   mcp.Operation
}

// toolsByOperation returns the tool that calls each operation, keyed as the matrices key operations.
func toolsByOperation(t *testing.T) map[openapitest.Operation]toolOperation {
	t.Helper()

	byOperation := map[openapitest.Operation]toolOperation{}
	for _, tool := range testMCPTools(t).Tools() {
		for _, op := range tool.Operations {
			key := openapitest.Operation{Method: op.Method, Path: op.Path}
			require.NotContains(t, byOperation, key, "two tools call %s %s", op.Method, op.Path)
			byOperation[key] = toolOperation{tool: tool, op: op}
		}
	}
	return byOperation
}

// mcpToolCallMessage is the JSON-RPC message that calls tool with args.
func mcpToolCallMessage(t *testing.T, tool string, args map[string]any) string {
	t.Helper()

	return encodeMessage(t, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args},
	})
}

func encodeMessage(t *testing.T, v any) string {
	t.Helper()

	encoded, err := json.Marshal(v)
	require.NoError(t, err)
	return string(encoded)
}

// newMCPRequest is a POST /v1/mcp request with message and the headers Streamable HTTP needs, as authorization.
func newMCPRequest(ctx context.Context, authorization, message string) *http.Request {
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, mcpPath, strings.NewReader(message))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", mcpAccept)
	req.Header.Set(mcpProtocolHeader, mcpProtocol)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return req
}

// mcpToolResult is a tools/call result.
type mcpToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent"`
	IsError           bool            `json:"isError"`
}

// decodeToolResult decodes the tool result of a 200 response to a tools/call.
func decodeToolResult(t *testing.T, rec *httptest.ResponseRecorder) mcpToolResult {
	t.Helper()

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var response struct {
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Empty(t, response.Error, "a JSON-RPC error: %s", rec.Body.String())
	var result mcpToolResult
	require.NoError(t, json.Unmarshal(response.Result, &result))
	return result
}

// text is the result's only content, a text.
func (r mcpToolResult) text(t *testing.T) string {
	t.Helper()

	require.Len(t, r.Content, 1)
	assert.Equal(t, "text", r.Content[0].Type)
	return r.Content[0].Text
}

// toolError is a tool error's text, decoded: the REST error body with the status.
func (r mcpToolResult) toolError(t *testing.T) map[string]any {
	t.Helper()

	require.True(t, r.IsError, "the call succeeded: %s", r.text(t))
	assert.Empty(t, r.StructuredContent, "a tool error has no structured content")
	var decoded map[string]any
	require.NoError(t, json.Unmarshal([]byte(r.text(t)), &decoded))
	return decoded
}

// specParameters are the spec's parameters, by operation and name.
var specParameters = sync.OnceValues(func() (map[openapitest.Operation]map[string]*openapi3.Parameter, error) {
	doc, err := openapi3.NewLoader().LoadFromData(openapi.Spec)
	if err != nil {
		return nil, err
	}
	parameters := map[openapitest.Operation]map[string]*openapi3.Parameter{}
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			byName := map[string]*openapi3.Parameter{}
			for _, p := range op.Parameters {
				byName[p.Value.Name] = p.Value
			}
			parameters[openapitest.Operation{Method: method, Path: path}] = byName
		}
	}
	return parameters, nil
})

// toolArguments returns the arguments of the call of the tool of to that makes the REST request to target, a path
// with an optional query, with body, which may be empty. Each path and query parameter becomes an argument of its
// type in the spec, and a tool with an operation per entity type gets the entity type of the operation's path.
func toolArguments(t *testing.T, to toolOperation, target, body string) map[string]any {
	t.Helper()

	all, err := specParameters()
	require.NoError(t, err)
	parameters := all[openapitest.Operation{Method: to.op.Method, Path: to.op.Path}]

	path, rawQuery, _ := strings.Cut(target, "?")
	args := map[string]any{}
	segments, templateSegments := strings.Split(path, "/"), strings.Split(to.op.Path, "/")
	require.Len(t, segments, len(templateSegments), "%s is not a path of %s", target, to.op.Path)
	for i, segment := range templateSegments {
		name, isParameter := strings.CutPrefix(segment, "{")
		if !isParameter {
			require.Equal(t, segment, segments[i], "%s is not a path of %s", target, to.op.Path)
			continue
		}
		name = strings.TrimSuffix(name, "}")
		value, err := url.PathUnescape(segments[i])
		require.NoError(t, err)
		args[name] = argumentValue(t, parameters[name], []string{value})
	}

	query, err := url.ParseQuery(rawQuery)
	require.NoError(t, err)
	for name, values := range query {
		args[name] = argumentValue(t, parameters[name], values)
	}

	if body != "" {
		args[bodyArgument] = json.RawMessage(body)
	}
	if len(to.tool.Operations) > 1 {
		args[entityTypeArgument] = strings.TrimSuffix(templateSegments[2], "s")
	}
	return args
}

// argumentValue is the argument of parameter p with values: an array of them if p is an array, otherwise the
// only one, as an integer, a boolean or a string by p's type.
func argumentValue(t *testing.T, p *openapi3.Parameter, values []string) any {
	t.Helper()

	require.NotNil(t, p, "the operation has no such parameter")
	schema := p.Schema.Value
	if schema.Type.Is(openapi3.TypeArray) {
		elements := make([]any, len(values))
		for i, value := range values {
			elements[i] = scalarArgument(t, schema.Items.Value, value)
		}
		return elements
	}
	require.Len(t, values, 1, "parameter %s", p.Name)
	return scalarArgument(t, schema, values[0])
}

func scalarArgument(t *testing.T, schema *openapi3.Schema, value string) any {
	t.Helper()

	switch {
	case schema.Type.Is(openapi3.TypeInteger):
		n, err := strconv.ParseInt(value, 10, 64)
		require.NoError(t, err)
		return n
	case schema.Type.Is(openapi3.TypeBoolean):
		b, err := strconv.ParseBool(value)
		require.NoError(t, err)
		return b
	default:
		return value
	}
}
