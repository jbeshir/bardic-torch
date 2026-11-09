package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// MaxBodyBytes is the largest MCP request the server reads: a tool call carrying the largest REST body, a
// document, and room for the JSON-RPC envelope around it.
const MaxBodyBytes = httpjson.MaxDocumentBodyBytes + 4<<10

// serverName is the name the server gives in its implementation info.
const serverName = "bardic-torch"

// protocolVersions are the MCP protocol versions the server serves, newest first: 2026-07-28 sessionless, and the
// handshake versions from 2025-06-18, the first without JSON-RPC batches.
func protocolVersions() []string {
	return []string{"2026-07-28", "2025-11-25", "2025-06-18"}
}

// The MCP methods that need tools.
const (
	methodListTools = "tools/list"
	methodCallTool  = "tools/call"
)

// handler serves MCP requests over stateless Streamable HTTP.
type handler struct {
	tools  *Toolset
	inner  http.Handler
	lookup Lookup
}

// NewHandler returns the handler of POST /v1/mcp, which serves the tools of tools by dispatching each call to
// inner, the API's router, as the REST request of the tool's operation; lookup is the router's, and pins each
// request to that operation's route. The caller authenticates requests before they reach it.
//
// It answers, as JSON errors, a body that is not application/json (415), an Accept without both
// application/json and text/event-stream (406), a body over MaxBodyBytes (413), a JSON-RPC batch (400), and a
// request whose context has already ended (503). The MCP SDK answers the rest, with JSON-RPC responses and
// plain-text errors of its own.
func NewHandler(tools *Toolset, inner http.Handler, lookup Lookup) http.Handler {
	return handler{tools: tools, inner: inner, lookup: lookup}
}

func (h handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		httpjson.WriteError(ctx, w, http.StatusUnsupportedMediaType, httpjson.CodeUnsupportedMediaType,
			"request body must be sent as application/json")
		return
	}
	if !acceptsBoth(r.Header.Values("Accept")) {
		httpjson.WriteError(ctx, w, http.StatusNotAcceptable, httpjson.CodeNotAcceptable,
			"the Accept header must list both application/json and text/event-stream")
		return
	}
	body, ok := readBody(ctx, w, r)
	if !ok {
		return
	}
	// One message per request, so that one request spends the MCP rate limit for one tool call at most. The SDK
	// would serve a batch under a missing protocol header, which it reads as 2025-03-26.
	if isBatch(body) {
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			"JSON-RPC batches are not supported")
		return
	}
	// The SDK writes nothing once its request's context has ended, so a budget already spent, as by a slow
	// upload, is answered here.
	if ctx.Err() != nil {
		httpjson.WriteError(ctx, w, http.StatusServiceUnavailable, httpjson.CodeUnavailable,
			"the request took too long; try again")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	// The SDK runs on a context the request budget does not end, so that a tool call whose operation answers after
	// the budget, as a write committed near it does, still gets that answer to the client; tool calls dispatch on
	// the request's own context, so an operation past the budget answers 503 itself. A client gone still ends it.
	sdkCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	stop := context.AfterFunc(ctx, func() {
		if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			cancel()
		}
	})
	defer stop()

	server := h.tools.server(dispatcher{outer: r, inner: h.inner, lookup: h.lookup}, body)
	sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, &sdk.StreamableHTTPOptions{
		Stateless:    true,
		JSONResponse: true,
		// A tool call is cancelled with its request, on every protocol version that allows it.
		PropagateRequestCancellation: true,
		// The API is never a local MCP server, and bearer tokens are never ambient, so neither DNS rebinding nor
		// cross-origin protection applies; behind a proxy every connection would otherwise look local.
		DisableLocalhostProtection: true,
		MaxRequestBodyBytes:        MaxBodyBytes,
	}).ServeHTTP(&noStoreWriter{ResponseWriter: w}, r.WithContext(sdkCtx))
}

// isBatch reports whether body is a JSON-RPC batch: a JSON array.
func isBatch(body []byte) bool {
	trimmed := bytes.TrimLeft(body, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '['
}

// noStoreWriter keeps the API's Cache-Control: no-store on the SDK's responses, which set their own: tool results
// hold the caller's own data.
type noStoreWriter struct {
	http.ResponseWriter
}

func (w *noStoreWriter) WriteHeader(status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.ResponseWriter.WriteHeader(status)
}

func (w *noStoreWriter) Write(p []byte) (int, error) {
	w.Header().Set("Cache-Control", "no-store")
	return w.ResponseWriter.Write(p)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *noStoreWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// readBody reads the request body, of at most MaxBodyBytes. If it cannot, it writes the error response (413 for
// a body too large, 400 otherwise) and returns false.
func readBody(ctx context.Context, w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &tooLarge):
		httpjson.WriteError(ctx, w, http.StatusRequestEntityTooLarge, httpjson.CodeRequestTooLarge,
			"request body is too large")
		return nil, false
	case err != nil:
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest, "unable to read the request body")
		return nil, false
	}
	return body, true
}

// acceptsBoth reports whether the Accept header values accept both application/json and text/event-stream, as
// Streamable HTTP requires, by the MCP SDK's rules: parameters are ignored, and wildcards count.
func acceptsBoth(values []string) bool {
	var jsonOK, streamOK bool
	for _, value := range values {
		for _, token := range strings.Split(value, ",") {
			base, _, _ := strings.Cut(token, ";")
			switch strings.ToLower(strings.TrimSpace(base)) {
			case "application/json", "application/*":
				jsonOK = true
			case "text/event-stream", "text/*":
				streamOK = true
			case "*/*":
				jsonOK, streamOK = true, true
			}
		}
	}
	return jsonOK && streamOK
}

// server is a new MCP server for one request, whose JSON-RPC message is body, with the tools it needs calling
// through d.
func (ts *Toolset) server(d dispatcher, body []byte) *sdk.Server {
	server := sdk.NewServer(&sdk.Implementation{Name: serverName, Version: ts.version}, &sdk.ServerOptions{
		// Tools only: no logging, prompts or resources, and the tool list never changes.
		Capabilities:              &sdk.ServerCapabilities{Tools: &sdk.ToolCapabilities{}},
		SupportedProtocolVersions: protocolVersions(),
	})
	for _, t := range ts.toolsFor(body) {
		server.AddTool(t.sdkTool, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
			return d.call(t, req.Params.Arguments), nil
		})
	}
	return server
}

// toolsFor returns the tools the server needs to answer the JSON-RPC message body: every tool to list them, the
// tool a call names (none if no tool has that name, which the SDK answers as unknown), and none for any other
// method. Registering a tool costs a pass over its input schema, tens of kilobytes for the draft tools, so a call
// pays only for its own tool. A body that does not decode gets every tool, and the SDK's answer.
func (ts *Toolset) toolsFor(body []byte) []tool {
	var message struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &message) != nil {
		return ts.tools
	}

	switch message.Method {
	case methodListTools:
		return ts.tools
	case methodCallTool:
		for _, t := range ts.tools {
			if t.Name == message.Params.Name {
				return []tool{t}
			}
		}
	}
	return nil
}
