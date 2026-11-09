package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Lookup returns the path template of the route a request is routed to, or false if no route matches it or its
// path is not in canonical form.
type Lookup func(*http.Request) (string, bool)

// The fields of a tool error's text: the REST error body's code and message, and the REST status, which is also
// a field of a call's log line; on 429, the seconds to wait.
const (
	statusField     = "status"
	codeField       = "code"
	messageField    = "message"
	retryAfterField = "retry_after_seconds"
)

// unencodableResult is the text of a result that could not be encoded, which a REST response decoded as JSON
// never is.
func unencodableResult() string {
	return fmt.Sprintf(`{%q:%d,%q:%q,%q:%q}`, statusField, http.StatusInternalServerError, codeField,
		httpjson.CodeInternal, messageField, "unable to encode the result")
}

// notAddressedMessage answers arguments that would make a request to anything but the tool's own operation.
const notAddressedMessage = "the arguments do not address the tool's operation"

// dispatcher calls tools for one MCP request, outer: as REST requests to inner, with outer's context and
// credentials.
type dispatcher struct {
	outer  *http.Request
	inner  http.Handler
	lookup Lookup
}

// call calls t with the raw arguments rawArgs and returns the tool result. It logs one line through the outer
// request's logger: the tool, the inner route if a request was made, and the status. Neither arguments nor
// results are logged.
func (d dispatcher) call(t tool, rawArgs json.RawMessage) *sdk.CallToolResult {
	ctx := d.outer.Context()
	status, route, result := d.dispatch(t, rawArgs)

	attrs := []any{"tool", t.Name, statusField, status}
	if route != "" {
		attrs = append(attrs, "tool_route", route)
	}
	domain.LoggerFromContext(ctx).InfoContext(ctx, "mcp tool called", attrs...)

	return result
}

// dispatch validates the arguments, makes the REST request they address and returns its status, the route it was
// served by (empty if none), and the tool result.
func (d dispatcher) dispatch(t tool, rawArgs json.RawMessage) (int, string, *sdk.CallToolResult) {
	args, err := decodeArguments(rawArgs)
	if err != nil {
		return refuse(err)
	}
	op, err := t.operationFor(args)
	if err != nil {
		return refuse(err)
	}
	_, single := t.operations[""]
	if err := op.validate(args, !single); err != nil {
		return refuse(err)
	}

	return d.serve(op, args)
}

// serve makes the REST request of op that args address, and returns its status, the route it was served by
// (empty if none), and the tool result.
func (d dispatcher) serve(op operation, args arguments) (int, string, *sdk.CallToolResult) {
	req, err := d.request(op, args)
	if err != nil {
		return refuse(err)
	}
	// The request must be routed to the tool's own operation, as built: never redirected, and never to another
	// route an argument's value reaches.
	if template, ok := d.lookup(req); !ok || template != op.path {
		return refuse(&argumentError{message: notAddressedMessage})
	}

	rec := &responseBuffer{header: http.Header{}, status: http.StatusOK}
	d.inner.ServeHTTP(rec, req)
	return rec.status, op.path, rec.result()
}

// refuse answers arguments the tool refuses with a 400 invalid_request tool error.
func refuse(err error) (int, string, *sdk.CallToolResult) {
	message := notAddressedMessage
	if argErr, ok := asArgumentError(err); ok {
		message = argErr.message
	}
	return http.StatusBadRequest, "", errorResult(object{
		statusField: http.StatusBadRequest, codeField: httpjson.CodeInvalidRequest, messageField: message,
	})
}

// request builds the REST request of op from args: path parameters percent-encoded into the path template, query
// parameters in the query string, and the body. It carries the outer request's context, credentials and peer.
func (d dispatcher) request(op operation, args arguments) (*http.Request, error) {
	path := op.path
	query := url.Values{}
	for _, p := range op.parameters {
		raw, ok := args.raw[p.name]
		if !ok {
			continue
		}
		values, err := parameterValues(p, raw)
		if err != nil {
			return nil, err
		}
		if p.in == "path" {
			path = strings.ReplaceAll(path, "{"+p.name+"}", url.PathEscape(values[0]))
			continue
		}
		query[p.name] = values
	}
	target := path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var body []byte
	if raw, ok := args.raw[bodyArgument]; ok {
		body = raw
	}
	req, err := http.NewRequestWithContext(d.outer.Context(), op.method, target, bytes.NewReader(body))
	if err != nil {
		return nil, &argumentError{message: notAddressedMessage}
	}
	req.RemoteAddr = d.outer.RemoteAddr
	req.Header.Set("Authorization", d.outer.Header.Get("Authorization"))
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// parameterValues returns the values a parameter's argument sends: one, or one per element of an array.
func parameterValues(p parameter, raw json.RawMessage) ([]string, error) {
	if !p.array {
		value, err := scalarValue(p.name, raw)
		return []string{value}, err
	}

	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil {
		return nil, &argumentError{message: "invalid argument: " + p.name}
	}
	values := make([]string, len(elements))
	for i, element := range elements {
		value, err := scalarValue(p.name, element)
		if err != nil {
			return nil, err
		}
		values[i] = value
	}
	return values, nil
}

// scalarValue returns the text a string, integer or boolean argument is sent as. An integer is sent in decimal
// digits, as the REST operation reads it, however its JSON number is written: 1e3 and 1000.0 are 1000. A number
// that is not an exact integer within int64 is refused.
func scalarValue(name string, raw json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(raw)
	var text string
	switch {
	case bytes.HasPrefix(trimmed, []byte(`"`)) && json.Unmarshal(trimmed, &text) == nil:
		return text, nil
	case string(trimmed) == "true" || string(trimmed) == "false":
		return string(trimmed), nil
	}
	if digits, ok := integerDigits(trimmed); ok {
		return digits, nil
	}
	return "", &argumentError{message: "invalid argument: " + name}
}

// The bounds of a JSON number read as an integer, which an int64 is well within, so that its exact value is cheap:
// neither a number of a megabyte of digits nor a short one such as 1e999999999 is ever worked out.
const (
	maxIntegerLength   = 64
	maxIntegerExponent = 64
)

// integerDigits returns the decimal digits of the JSON number number, if it is an exact integer within int64.
func integerDigits(number []byte) (string, bool) {
	if !isBoundedNumber(number) {
		return "", false
	}
	value, ok := new(big.Rat).SetString(string(number))
	if !ok || !value.IsInt() || !value.Num().IsInt64() {
		return "", false
	}
	return value.Num().String(), true
}

// isBoundedNumber reports whether number is a JSON number within maxIntegerLength and maxIntegerExponent.
func isBoundedNumber(number []byte) bool {
	if len(number) == 0 || len(number) > maxIntegerLength || !json.Valid(number) {
		return false
	}
	if first := number[0]; first != '-' && (first < '0' || first > '9') {
		return false
	}
	i := bytes.IndexAny(number, "eE")
	if i < 0 {
		return true
	}
	exponent, err := strconv.Atoi(string(number[i+1:]))
	return err == nil && exponent >= -maxIntegerExponent && exponent <= maxIntegerExponent
}

// responseBuffer records the REST response to a tool call.
type responseBuffer struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (b *responseBuffer) Header() http.Header {
	return b.header
}

func (b *responseBuffer) WriteHeader(status int) {
	if !b.wroteHeader {
		b.status = status
		b.wroteHeader = true
	}
}

func (b *responseBuffer) Write(p []byte) (int, error) {
	b.wroteHeader = true
	n, err := b.body.Write(p)
	if err != nil {
		return n, fmt.Errorf("buffering the response: %w", err)
	}
	return n, nil
}

// result is the tool result of the REST response. A 2xx with a body is the body, as structured content and as
// text; one without a body is its status as text. Any other status is an error: the REST error body with the
// status, and on 429 the seconds to wait, as text only.
func (b *responseBuffer) result() *sdk.CallToolResult {
	body := bytes.TrimSpace(b.body.Bytes())
	switch {
	case b.status >= http.StatusOK && b.status < http.StatusMultipleChoices && len(body) == 0:
		return textResult(object{statusField: b.status})
	case b.status >= http.StatusOK && b.status < http.StatusMultipleChoices:
		return &sdk.CallToolResult{
			Content:           []sdk.Content{&sdk.TextContent{Text: string(body)}},
			StructuredContent: json.RawMessage(body),
		}
	}

	errorBody := object{}
	if err := json.Unmarshal(body, &errorBody); err != nil {
		errorBody = object{codeField: httpjson.CodeInternal, messageField: "the operation answered without an error body"}
	}
	errorBody[statusField] = b.status
	if b.status == http.StatusTooManyRequests {
		if seconds, err := strconv.Atoi(b.header.Get("Retry-After")); err == nil {
			errorBody[retryAfterField] = seconds
		}
	}
	return errorResult(errorBody)
}

// errorResult is a tool error: body as JSON text, without structured content.
func errorResult(body object) *sdk.CallToolResult {
	result := textResult(body)
	result.IsError = true
	return result
}

func textResult(body object) *sdk.CallToolResult {
	text, err := json.Marshal(body)
	if err != nil {
		text = []byte(unencodableResult())
	}
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: string(text)}}}
}
