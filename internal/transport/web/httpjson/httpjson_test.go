package httpjson

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	mediaType = "application/json"
	testName  = "lute"

	mediaTypeMessage = "request body must be sent as application/json"
	tooLargeMessage  = "request body is too large"
	notJSONMessage   = "request body is not valid JSON"
	notObjectMessage = "request body must be a JSON object"
)

type testBody struct {
	Name   string `json:"name"`
	Detail *struct {
		Count int `json:"count"`
	} `json:"detail,omitempty"`
}

func TestWriteJSON(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	WriteJSON(t.Context(), rec, http.StatusCreated, testBody{Name: testName})

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, mediaType, rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"name":"lute"}`, rec.Body.String())
}

func TestWriteJSON_EncodingFailureKeepsStatus(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	WriteJSON(t.Context(), rec, http.StatusOK, math.Inf(1))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, rec.Body.String())
}

func TestWriteError(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	WriteError(t.Context(), rec, http.StatusServiceUnavailable, CodeUnavailable, "try again later")

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, mediaType, rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"code":"unavailable","message":"try again later"}`, rec.Body.String())
}

func TestDecodeJSONBody(t *testing.T) {
	t.Parallel()

	atLimit := `{"name":"` + strings.Repeat("a", MaxBodyBytes-len(`{"name":""}`)) + `"}`
	overLimit := `{"name":"` + strings.Repeat("a", MaxBodyBytes) + `"}`

	cases := []struct {
		name        string
		contentType string
		body        string
		want        testBody
		wantStatus  int
		wantCode    ErrorCode
		wantMessage string
	}{
		{name: "valid", contentType: mediaType, body: `{"name":"lute"}`, want: testBody{Name: testName}},
		{name: "empty_object", contentType: mediaType, body: `{}`},
		{name: "surrounding_whitespace", contentType: mediaType, body: " \n" + `{"name":"lute"}` + "\n ", want: testBody{Name: testName}},
		{name: "charset_parameter", contentType: "application/json; charset=utf-8", body: `{"name":"lute"}`, want: testBody{Name: testName}},
		{name: "at_size_limit", contentType: mediaType, body: atLimit, want: testBody{Name: atLimit[9 : len(atLimit)-2]}},
		{name: "missing_content_type", body: `{}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: CodeUnsupportedMediaType, wantMessage: mediaTypeMessage},
		{name: "wrong_content_type", contentType: "text/plain", body: `{}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: CodeUnsupportedMediaType, wantMessage: mediaTypeMessage},
		{name: "json_suffix_type", contentType: "application/merge-patch+json", body: `{}`, wantStatus: http.StatusUnsupportedMediaType, wantCode: CodeUnsupportedMediaType, wantMessage: mediaTypeMessage},
		{name: "over_size_limit", contentType: mediaType, body: overLimit, wantStatus: http.StatusRequestEntityTooLarge, wantCode: CodeRequestTooLarge, wantMessage: tooLargeMessage},
		{name: "trailing_data_over_size_limit", contentType: mediaType, body: `{}` + strings.Repeat(" ", MaxBodyBytes), wantStatus: http.StatusRequestEntityTooLarge, wantCode: CodeRequestTooLarge, wantMessage: tooLargeMessage},
		{name: "empty", contentType: mediaType, body: ``, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: "request body must not be empty"},
		{name: "malformed", contentType: mediaType, body: `{"name":`, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: notJSONMessage},
		{name: "unknown_field", contentType: mediaType, body: `{"name":"lute","extra":1}`, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: "request body has an unknown field"},
		{name: "wrong_field_type", contentType: mediaType, body: `{"name":5}`, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: "field name has the wrong type"},
		{name: "wrong_nested_field_type", contentType: mediaType, body: `{"detail":{"count":"x"}}`, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: "field detail.count has the wrong type"},
		{name: "invalid_syntax", contentType: mediaType, body: `{"name" "lute"}`, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: notJSONMessage},
		{name: "second_value", contentType: mediaType, body: `{}{}`, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: "request body must be a single JSON value"},
		{name: "trailing_garbage", contentType: mediaType, body: `{} x`, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: notJSONMessage},
		{name: "array", contentType: mediaType, body: `[]`, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: notObjectMessage},
		{name: "null", contentType: mediaType, body: `null`, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: notObjectMessage},
		{name: "string", contentType: mediaType, body: `"lute"`, wantStatus: http.StatusBadRequest, wantCode: CodeInvalidRequest, wantMessage: notObjectMessage},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tc.body))
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			rec := httptest.NewRecorder()

			var got testBody
			ok := DecodeJSONBody(rec, req, &got)

			if tc.wantStatus == 0 {
				require.True(t, ok, "unexpected error response: %s", rec.Body.String())
				assert.Equal(t, tc.want, got)
				assert.Equal(t, http.StatusOK, rec.Code, "nothing should be written on success")
				assert.Empty(t, rec.Body.String())
				return
			}

			require.False(t, ok)
			assert.Equal(t, tc.wantStatus, rec.Code)
			var body errorBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, tc.wantCode, body.Code)
			assert.Equal(t, tc.wantMessage, body.Message)
		})
	}
}

func TestDecodeJSONBody_LogsDecodeErrorAtDebug(t *testing.T) {
	t.Parallel()

	for _, body := range []string{`{"name":5}`, `{"name":"lute","extra":1}`} {
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		ctx := domain.ContextWithLogger(t.Context(), logger)
		req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/", strings.NewReader(body))
		req.Header.Set("Content-Type", mediaType)

		var got testBody
		require.False(t, DecodeJSONBody(httptest.NewRecorder(), req, &got), body)

		var entry map[string]any
		require.NoError(t, json.Unmarshal(logs.Bytes(), &entry), body)
		assert.Equal(t, "DEBUG", entry["level"], body)
		assert.Contains(t, entry["error"], "json: ", body)
	}
}

func TestDecodeJSONBodyLimit(t *testing.T) {
	t.Parallel()

	const limit = 100
	pad := func(n int) string { return `{"name":"` + strings.Repeat("a", n-len(`{"name":""}`)) + `"}` }

	cases := []struct {
		name       string
		limit      int64
		body       string
		wantStatus int
	}{
		{name: "at_limit", limit: limit, body: pad(limit)},
		{name: "one_byte_over_limit", limit: limit, body: pad(limit + 1), wantStatus: http.StatusRequestEntityTooLarge},
		{name: "default_limit_is_not_used", limit: MaxBodyBytes + 1, body: pad(MaxBodyBytes + 1)},
		{name: "document_limit", limit: MaxDocumentBodyBytes, body: pad(MaxDocumentBodyBytes)},
		{name: "over_document_limit", limit: MaxDocumentBodyBytes, body: pad(MaxDocumentBodyBytes + 1), wantStatus: http.StatusRequestEntityTooLarge},
		{name: "contact_limit", limit: MaxContactBodyBytes, body: pad(MaxContactBodyBytes)},
		{name: "over_contact_limit", limit: MaxContactBodyBytes, body: pad(MaxContactBodyBytes + 1), wantStatus: http.StatusRequestEntityTooLarge},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", mediaType)
			rec := httptest.NewRecorder()

			var got testBody
			ok := DecodeJSONBodyLimit(rec, req, &got, tc.limit)

			if tc.wantStatus == 0 {
				require.True(t, ok, "unexpected error response: %s", rec.Body.String())
				assert.Len(t, tc.body, len(`{"name":""}`)+len(got.Name))
				assert.Empty(t, rec.Body.String())
				return
			}

			require.False(t, ok)
			assert.Equal(t, tc.wantStatus, rec.Code)
			var body errorBody
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, CodeRequestTooLarge, body.Code)
		})
	}
}

func TestDecodeJSONField(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		raw         string
		want        testBody
		wantMessage string
	}{
		{name: "object", raw: `{"name":"Morwen","detail":{"count":2}}`,
			want: testBody{Name: "Morwen", Detail: &struct {
				Count int `json:"count"`
			}{Count: 2}}},
		{name: "missing", raw: ``, wantMessage: notObjectField},
		{name: "boolean", raw: `true`, wantMessage: notObjectField},
		{name: "array", raw: `[]`, wantMessage: notObjectField},
		{name: "unknown_field", raw: `{"name":"Morwen","extra":1}`, wantMessage: "field document has an unknown field"},
		{name: "wrong_type", raw: `{"name":1}`, wantMessage: "field document.name has the wrong type"},
		{name: "nested_wrong_type", raw: `{"detail":{"count":"2"}}`,
			wantMessage: "field document.detail.count has the wrong type"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			var got testBody
			ok := DecodeJSONField(t.Context(), rec, json.RawMessage(tc.raw), "document", &got)

			if tc.wantMessage == "" {
				require.True(t, ok, "unexpected error response: %s", rec.Body.String())
				assert.Equal(t, tc.want, got)
				return
			}
			require.False(t, ok)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			assert.JSONEq(t, `{"code":"invalid_request","message":"`+tc.wantMessage+`"}`, rec.Body.String())
		})
	}
}

const notObjectField = "field document must be a JSON object"
