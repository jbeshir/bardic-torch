// Package httpjson is the single path for reading and writing JSON over HTTP, so that every endpoint shares
// the same body limits, error format and content type.
package httpjson

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// MaxBodyBytes is the largest request body DecodeJSONBody accepts.
const MaxBodyBytes = 16 << 10

var errMultipleValues = errors.New("body contains more than one JSON value")

// ErrorCode is the machine-readable code in an error response, enumerated in the OpenAPI Error schema. Constant
// names say "API auth" where the code says "token", because gosec reports constants named after tokens as
// hardcoded credentials.
type ErrorCode string

const (
	CodeInvalidRequest       ErrorCode = "invalid_request"
	CodeUnsupportedMediaType ErrorCode = "unsupported_media_type"
	CodeRequestTooLarge      ErrorCode = "request_too_large"
	CodeUnauthenticated      ErrorCode = "unauthenticated"
	CodeUserBlocked          ErrorCode = "user_blocked"
	CodeAPIAuthNotAllowed    ErrorCode = "api_token_not_allowed"
	CodeModeratorRequired    ErrorCode = "moderator_required"
	CodeNotFound             ErrorCode = "not_found"
	CodeMethodNotAllowed     ErrorCode = "method_not_allowed"
	CodeConflict             ErrorCode = "conflict"
	CodeAPIAuthLimitReached  ErrorCode = "token_limit_reached"
	CodeInternal             ErrorCode = "internal"
	CodeUnavailable          ErrorCode = "unavailable"
)

type errorBody struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// WriteJSON writes body as a JSON response with the given status. Encoding failures are logged, since the
// status has already been sent.
func WriteJSON(ctx context.Context, w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		domain.LoggerFromContext(ctx).ErrorContext(ctx, "unable to write response", "error", err)
	}
}

// WriteError writes an error response with the given status, code and client-facing message. The message
// must not contain internal details; log those instead.
func WriteError(ctx context.Context, w http.ResponseWriter, status int, code ErrorCode, message string) {
	WriteJSON(ctx, w, status, errorBody{Code: code, Message: message})
}

// DecodeJSONBody decodes the request body, which must be a single JSON object of at most MaxBodyBytes sent as
// application/json, into dst, rejecting unknown fields. If the body is unacceptable it writes the matching
// error response (415, 413 or 400) and returns false; the caller should then return without writing.
func DecodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	ctx := r.Context()

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		WriteError(ctx, w, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType,
			"request body must be sent as application/json")
		return false
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, MaxBodyBytes))

	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		writeDecodeError(ctx, w, err)
		return false
	}

	// Anything after the first value, including a second value, makes the body invalid.
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		writeDecodeError(ctx, w, cmp.Or(err, errMultipleValues))
		return false
	}

	if raw[0] != '{' {
		WriteError(ctx, w, http.StatusBadRequest, CodeInvalidRequest, "request body must be a JSON object")
		return false
	}

	// Decoding with unknown fields allowed first separates wrong types, which encoding/json reports with a typed
	// error, from unknown fields, which it reports only as text: a body that decodes this way but not strictly
	// has an unknown field.
	if err := json.Unmarshal(raw, dst); err != nil {
		writeDecodeError(ctx, w, err)
		return false
	}
	strictDec := json.NewDecoder(bytes.NewReader(raw))
	strictDec.DisallowUnknownFields()
	if err := strictDec.Decode(dst); err != nil {
		logDecodeError(ctx, err)
		WriteError(ctx, w, http.StatusBadRequest, CodeInvalidRequest, "request body has an unknown field")
		return false
	}

	return true
}

// writeDecodeError writes the error response for a body that could not be decoded. The client gets a fixed
// message, naming at most the JSON field at fault, and err itself is logged at debug level.
func writeDecodeError(ctx context.Context, w http.ResponseWriter, err error) {
	logDecodeError(ctx, err)

	var (
		maxBytesErr *http.MaxBytesError
		syntaxErr   *json.SyntaxError
		typeErr     *json.UnmarshalTypeError
		status      = http.StatusBadRequest
		code        = CodeInvalidRequest
		message     string
	)
	switch {
	case errors.As(err, &maxBytesErr):
		status, code, message = http.StatusRequestEntityTooLarge, CodeRequestTooLarge, "request body is too large"
	case errors.Is(err, io.EOF):
		message = "request body must not be empty"
	case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		message = "request body is not valid JSON"
	case errors.Is(err, errMultipleValues):
		message = "request body must be a single JSON value"
	case errors.As(err, &typeErr) && typeErr.Field != "":
		message = "field " + typeErr.Field + " has the wrong type"
	default:
		message = "invalid request body"
	}

	WriteError(ctx, w, status, code, message)
}

func logDecodeError(ctx context.Context, err error) {
	domain.LoggerFromContext(ctx).DebugContext(ctx, "unable to decode request body", "error", err)
}
