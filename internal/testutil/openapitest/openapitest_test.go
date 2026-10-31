package openapitest

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	songsPath = "/songs"
	songPath  = "/songs/abc"
	// songPathTemplate is the path of the operations on one song.
	songPathTemplate = "/songs/{song_id}"

	songJSON  = `{"id":"abc","title":"Lament","composer":null}`
	errorJSON = `{"code":"not_found","message":"no such song"}`
)

// recordingTB records failures reported through Errorf instead of failing the test, so that tests can assert
// the Validator reports them. Every other method goes to the wrapped real test.
type recordingTB struct {
	testing.TB
	failures []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

func newFixtureValidator(t *testing.T) *Validator {
	t.Helper()

	spec, err := os.ReadFile("testdata/contract.yaml")
	require.NoError(t, err)
	v, err := newValidator(spec)
	require.NoError(t, err)
	return v
}

// respond returns a handler writing status, content type and body.
func respond(status int, contentType, body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

func jsonResponse(status int, body string) http.Handler {
	return respond(status, "application/json", body)
}

type validatorCase struct {
	name         string
	method       string
	url          string
	body         string
	handler      http.Handler
	responseOnly bool
	// wantFailure is a substring of the expected failure; empty means the exchange must be accepted.
	wantFailure string
}

func TestValidator_Serve(t *testing.T) {
	t.Parallel()

	cases := []validatorCase{
		{
			name:    "accepts_conforming_response",
			method:  http.MethodGet,
			url:     songPath,
			handler: jsonResponse(http.StatusOK, songJSON),
		},
		{
			name:    "accepts_documented_error_status",
			method:  http.MethodGet,
			url:     songPath,
			handler: jsonResponse(http.StatusNotFound, errorJSON),
		},
		{
			name:    "accepts_documented_empty_response",
			method:  http.MethodDelete,
			url:     songPath,
			handler: respond(http.StatusNoContent, "", ""),
		},
		{
			name:    "accepts_request_to_arbitrary_host",
			method:  http.MethodGet,
			url:     "http://localhost:8080/songs/abc",
			handler: jsonResponse(http.StatusOK, songJSON),
		},
		{
			name:    "accepts_conforming_request_body",
			method:  http.MethodPost,
			url:     songsPath,
			body:    `{"title":"Lament"}`,
			handler: jsonResponse(http.StatusCreated, songJSON),
		},
		{
			name:        "rejects_undocumented_status",
			method:      http.MethodGet,
			url:         songPath,
			handler:     jsonResponse(http.StatusInternalServerError, errorJSON),
			wantFailure: "status is not supported",
		},
		{
			name:        "rejects_missing_required_field",
			method:      http.MethodGet,
			url:         songPath,
			handler:     jsonResponse(http.StatusOK, `{"id":"abc","composer":null}`),
			wantFailure: "missing property 'title'",
		},
		{
			name:        "rejects_extra_field",
			method:      http.MethodGet,
			url:         songPath,
			handler:     jsonResponse(http.StatusOK, `{"id":"abc","title":"Lament","composer":null,"extra":1}`),
			wantFailure: "additional properties 'extra' not allowed",
		},
		{
			name:        "rejects_wrong_type",
			method:      http.MethodGet,
			url:         songPath,
			handler:     jsonResponse(http.StatusOK, `{"id":"abc","title":7,"composer":null}`),
			wantFailure: "at '/title': got number, want string",
		},
		{
			name:        "rejects_invalid_enum_value",
			method:      http.MethodGet,
			url:         songPath,
			handler:     jsonResponse(http.StatusNotFound, `{"code":"missing","message":"no such song"}`),
			wantFailure: "at '/code': value must be one of",
		},
		{
			name:        "rejects_malformed_json",
			method:      http.MethodGet,
			url:         songPath,
			handler:     jsonResponse(http.StatusOK, `{"id":`),
			wantFailure: "does not conform",
		},
		{
			name:        "rejects_undocumented_content_type",
			method:      http.MethodGet,
			url:         songPath,
			handler:     respond(http.StatusOK, "text/plain", songJSON),
			wantFailure: "Content-Type",
		},
		{
			name:        "rejects_missing_content_type",
			method:      http.MethodGet,
			url:         songPath,
			handler:     respond(http.StatusOK, "", ""),
			wantFailure: "Content-Type",
		},
		{
			name:        "rejects_body_on_response_documented_without_one",
			method:      http.MethodDelete,
			url:         songPath,
			handler:     jsonResponse(http.StatusNoContent, songJSON),
			wantFailure: "documented without a body",
		},
		{
			name:        "rejects_undocumented_route",
			method:      http.MethodGet,
			url:         "/albums/abc",
			handler:     jsonResponse(http.StatusOK, songJSON),
			wantFailure: "is not an operation",
		},
		{
			name:        "rejects_undocumented_method",
			method:      http.MethodPut,
			url:         songPath,
			handler:     jsonResponse(http.StatusOK, songJSON),
			wantFailure: "is not an operation",
		},
		{
			name:        "rejects_invalid_path_parameter",
			method:      http.MethodGet,
			url:         "/songs/ABC",
			handler:     jsonResponse(http.StatusOK, songJSON),
			wantFailure: `parameter "song_id"`,
		},
		{
			name:        "rejects_invalid_request_body",
			method:      http.MethodPost,
			url:         songsPath,
			body:        `{"title":"A title that is far too long"}`,
			handler:     jsonResponse(http.StatusCreated, songJSON),
			wantFailure: "request POST /songs does not conform",
		},
		{
			name:         "response_only_skips_request_validation",
			method:       http.MethodPost,
			url:          songsPath,
			body:         `{"title":`,
			handler:      jsonResponse(http.StatusBadRequest, `{"code":"invalid_request","message":"bad body"}`),
			responseOnly: true,
		},
		{
			name:         "response_only_still_checks_response",
			method:       http.MethodPost,
			url:          songsPath,
			body:         `{"title":`,
			handler:      jsonResponse(http.StatusBadRequest, `{"message":"bad body"}`),
			responseOnly: true,
			wantFailure:  "missing property 'code'",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := newFixtureValidator(t)
			req := httptest.NewRequestWithContext(t.Context(), tc.method, tc.url, strings.NewReader(tc.body))
			if tc.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}

			recorder := &recordingTB{TB: t}
			serve := v.Serve
			if tc.responseOnly {
				serve = v.ServeResponseOnly
			}
			serve(recorder, tc.handler, req)

			if tc.wantFailure == "" {
				assert.Empty(t, recorder.failures)
				return
			}
			require.NotEmpty(t, recorder.failures, "expected the validator to report a failure")
			assert.Contains(t, strings.Join(recorder.failures, "\n"), tc.wantFailure)
		})
	}
}

func TestValidator_ServePassesBodyToHandlerAndReturnsResponse(t *testing.T) {
	t.Parallel()

	v := newFixtureValidator(t)
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/songs", strings.NewReader(`{"title":"Lament"}`))
	req.Header.Set("Content-Type", "application/json")

	var received string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		received = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, songJSON)
	})

	rec := v.Serve(t, handler, req)

	assert.JSONEq(t, `{"title":"Lament"}`, received)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.JSONEq(t, songJSON, rec.Body.String())
}

func TestValidator_Operations(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []Operation{
		{Method: http.MethodPost, Path: "/songs"},
		{Method: http.MethodDelete, Path: songPathTemplate},
		{Method: http.MethodGet, Path: songPathTemplate},
	}, newFixtureValidator(t).Operations())
}

// TestValidator_Public checks that only an operation whose security is explicitly empty is public: one that
// inherits the default (here, none) or names a scheme is not.
func TestValidator_Public(t *testing.T) {
	t.Parallel()

	v := newFixtureValidator(t)
	assert.True(t, v.Public(Operation{Method: http.MethodGet, Path: songPathTemplate}))
	assert.False(t, v.Public(Operation{Method: http.MethodDelete, Path: songPathTemplate}))
	assert.False(t, v.Public(Operation{Method: http.MethodPost, Path: "/songs"}))
	assert.False(t, v.Public(Operation{Method: http.MethodPut, Path: songPathTemplate}))
	assert.False(t, v.Public(Operation{Method: http.MethodGet, Path: "/albums"}))
}

func TestValidator_RejectsInvalidSpec(t *testing.T) {
	t.Parallel()

	_, err := newValidator([]byte("openapi: 3.1.0\ninfo: {title: broken}\npaths: {}\n"))
	assert.Error(t, err)
}

func TestNew_LoadsEmbeddedSpec(t *testing.T) {
	t.Parallel()

	first, second := New(t), New(t)
	assert.Same(t, first, second, "the embedded spec should be loaded once")
}
