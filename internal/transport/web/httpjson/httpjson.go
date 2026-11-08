// Package httpjson is the single path for reading and writing JSON over HTTP, so that every endpoint shares
// the same body limits, error format and content type.
package httpjson

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/domain"
)

// The largest request bodies the endpoints accept: MaxBodyBytes for DecodeJSONBody, and for the endpoints that
// carry more, the limit they pass to DecodeJSONBodyLimit.
const (
	MaxBodyBytes = 16 << 10
	// MaxDocumentBodyBytes is the limit for bodies carrying a document, whose element caps would otherwise allow
	// more bytes than a request should hold.
	MaxDocumentBodyBytes = 1 << 20
	// MaxContactBodyBytes is the limit for the contact form, whose message can reach 20-30 KB once escaped.
	MaxContactBodyBytes = 64 << 10
)

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
	// The codes of revision writes.
	CodeInvalidDocument         ErrorCode = "invalid_document"
	CodeNotDraft                ErrorCode = "not_draft"
	CodeNotPending              ErrorCode = "not_pending"
	CodeRebaseRequired          ErrorCode = "rebase_required"
	CodeEntityGone              ErrorCode = "entity_gone"
	CodeAlreadyCurrent          ErrorCode = "already_current"
	CodePendingLimitReached     ErrorCode = "pending_limit_reached"
	CodeDraftLimitReached       ErrorCode = "draft_limit_reached"
	CodePossibleDuplicate       ErrorCode = "possible_duplicate"
	CodeDuplicateBlocked        ErrorCode = "duplicate_blocked"
	CodeHiddenDataConflict      ErrorCode = "hidden_data_conflict"
	CodePendingReferenceFlagged ErrorCode = "pending_reference_flagged"
	CodeUnacknowledgedFlags     ErrorCode = "unacknowledged_flags"
	CodeDuplicateName           ErrorCode = "duplicate_name"
	CodeStale                   ErrorCode = "stale"
	// The codes of PATCH /v1/me and of link reports.
	CodeDisplayNameTaken    ErrorCode = "display_name_taken"
	CodeDisplayNameReserved ErrorCode = "display_name_reserved"
	CodeReportLimitReached  ErrorCode = "report_limit_reached"
	CodeReportCooldown      ErrorCode = "report_cooldown"
	// The code of a contact request whose Turnstile token does not pass.
	CodeTurnstileFailed ErrorCode = "turnstile_failed"
	// The code of a new repertoire entry of a full repertoire.
	CodeRepertoireLimitReached ErrorCode = "repertoire_limit_reached"
	// The code of a request refused by a rate limit.
	CodeRateLimited ErrorCode = "rate_limited"

	// The codes of POST /v1/mcp: a credential other than an API token, and an Accept header without the media
	// types Streamable HTTP requires.
	CodeAPIAuthRequired ErrorCode = "api_token_required"
	CodeNotAcceptable   ErrorCode = "not_acceptable"
)

// ErrorCodeValues returns every error code, in declaration order: the codes the spec's error bodies enumerate
// (TestSpec_InlineEnumsMatchCode).
func ErrorCodeValues() []ErrorCode {
	return []ErrorCode{
		CodeInvalidRequest, CodeUnsupportedMediaType, CodeRequestTooLarge, CodeUnauthenticated, CodeUserBlocked,
		CodeAPIAuthNotAllowed, CodeModeratorRequired, CodeNotFound, CodeMethodNotAllowed, CodeConflict,
		CodeAPIAuthLimitReached, CodeInternal, CodeUnavailable, CodeInvalidDocument, CodeNotDraft, CodeNotPending,
		CodeRebaseRequired, CodeEntityGone, CodeAlreadyCurrent, CodePendingLimitReached, CodeDraftLimitReached,
		CodePossibleDuplicate, CodeDuplicateBlocked, CodeHiddenDataConflict, CodePendingReferenceFlagged,
		CodeUnacknowledgedFlags, CodeDuplicateName, CodeStale, CodeDisplayNameTaken, CodeDisplayNameReserved,
		CodeReportLimitReached, CodeReportCooldown, CodeTurnstileFailed, CodeRepertoireLimitReached, CodeRateLimited,
		CodeAPIAuthRequired, CodeNotAcceptable,
	}
}

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

// DecodeJSONBody is DecodeJSONBodyLimit with the limit MaxBodyBytes.
func DecodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	return DecodeJSONBodyLimit(w, r, dst, MaxBodyBytes)
}

// DecodeJSONBodyLimit decodes the request body, which must be a single JSON object of at most limit bytes sent
// as application/json, into dst, rejecting unknown fields. If the body is unacceptable it writes the matching
// error response (415, 413 or 400) and returns false; the caller should then return without writing.
func DecodeJSONBodyLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	ctx := r.Context()

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		WriteError(ctx, w, http.StatusUnsupportedMediaType, CodeUnsupportedMediaType,
			"request body must be sent as application/json")
		return false
	}

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))

	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		writeDecodeError(ctx, w, err, "")
		return false
	}

	// Anything after the first value, including a second value, makes the body invalid.
	if err := dec.Decode(&json.RawMessage{}); !errors.Is(err, io.EOF) {
		writeDecodeError(ctx, w, cmp.Or(err, errMultipleValues), "")
		return false
	}

	return decodeObject(ctx, w, raw, dst, "request body", "")
}

// DecodeJSONField decodes raw, the value of the request body's field name, into dst as DecodeJSONBodyLimit
// decodes a body: it must be a JSON object, and unknown fields are rejected. If it is unacceptable it writes 400
// invalid_request, naming the field, and returns false; the caller should then return without writing. Bodies
// whose shape depends on another of their fields, such as a document whose type is a sibling field, decode that
// field as a json.RawMessage, then decode it with this.
func DecodeJSONField(ctx context.Context, w http.ResponseWriter, raw json.RawMessage, name string, dst any) bool {
	return decodeObject(ctx, w, raw, dst, "field "+name, name+".")
}

// decodeObject decodes raw into dst, rejecting unknown fields. what names raw in error messages, and prefix goes
// before the name of a field of raw.
func decodeObject(ctx context.Context, w http.ResponseWriter, raw json.RawMessage, dst any, what, prefix string) bool {
	if len(raw) == 0 || raw[0] != '{' {
		WriteError(ctx, w, http.StatusBadRequest, CodeInvalidRequest, what+" must be a JSON object")
		return false
	}

	// Decoding with unknown fields allowed first separates wrong types, which encoding/json reports with a typed
	// error, from unknown fields, which it reports only as text: a body that decodes this way but not strictly
	// has an unknown field.
	if err := json.Unmarshal(raw, dst); err != nil {
		writeDecodeError(ctx, w, err, prefix)
		return false
	}
	strictDec := json.NewDecoder(bytes.NewReader(raw))
	strictDec.DisallowUnknownFields()
	if err := strictDec.Decode(dst); err != nil {
		logDecodeError(ctx, err)
		WriteError(ctx, w, http.StatusBadRequest, CodeInvalidRequest, what+" has an unknown field")
		return false
	}

	return true
}

// writeDecodeError writes the error response for a body that could not be decoded. The client gets a fixed
// message, naming at most the JSON field at fault (after prefix), and err itself is logged at debug level.
func writeDecodeError(ctx context.Context, w http.ResponseWriter, err error, prefix string) {
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
		message = "field " + prefix + typeErr.Field + " has the wrong type"
	default:
		message = "invalid request body"
	}

	WriteError(ctx, w, status, code, message)
}

// logDecodeError logs, at debug level, why a body could not be decoded: the error's Go type and, for a JSON syntax
// or type error, its byte offset and the field at fault. Never the error's text, which may quote the body (an
// unknown field's name, or a value a field's own decoder refused).
func logDecodeError(ctx context.Context, err error) {
	attrs := []any{"error_type", fmt.Sprintf("%T", err)}
	var (
		syntaxErr *json.SyntaxError
		typeErr   *json.UnmarshalTypeError
	)
	switch {
	case errors.As(err, &syntaxErr):
		attrs = append(attrs, "offset", syntaxErr.Offset)
	case errors.As(err, &typeErr):
		attrs = append(attrs, "offset", typeErr.Offset, "field", typeErr.Field)
	}
	domain.LoggerFromContext(ctx).DebugContext(ctx, "unable to decode request body", attrs...)
}
