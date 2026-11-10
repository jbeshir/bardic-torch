package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/fakerepo"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/stretchr/testify/assert"
)

const emptyNameBody = `{"code":"invalid_request","message":"invalid display name: must not be empty"}`

func displayNameBody(name string) string {
	return `{"display_name":"` + name + `"}`
}

// TestMePatch_DisplayNameRules pins the display name rule against the spec: names are trimmed and NFC-normalised,
// and must be between 1 and 64 characters afterwards.
func TestMePatch_DisplayNameRules(t *testing.T) {
	t.Parallel()

	// composedE is "é" as one code point; decomposedE is "e" followed by a combining acute accent, which NFC
	// composes into composedE.
	const (
		composedE   = "\u00e9"
		decomposedE = "e\u0301"
	)
	sixtyFour := strings.Repeat("a", domain.MaxDisplayNameLength)

	cases := []struct {
		name string
		body string
		// invalidRequest marks requests that deliberately break the request schema, so only the response is
		// checked against the spec.
		invalidRequest bool
		wantStatus     int
		wantBody       string
	}{
		{name: "renamed", body: displayNameBody("Morwen of the Hills"), wantStatus: http.StatusOK,
			wantBody: `{"id":"google-oauth2|123","display_name":"Morwen of the Hills","role":"contributor"}`},
		{name: "padded_max_length", body: displayNameBody("  " + sixtyFour + "  "), wantStatus: http.StatusOK,
			wantBody: `{"id":"google-oauth2|123","display_name":"` + sixtyFour + `","role":"contributor"}`},
		{
			// 128 code points as sent, 64 once composed.
			name: "decomposed_max_length", body: displayNameBody(strings.Repeat(decomposedE, domain.MaxDisplayNameLength)),
			wantStatus: http.StatusOK,
			wantBody: `{"id":"google-oauth2|123","display_name":"` +
				strings.Repeat(composedE, domain.MaxDisplayNameLength) + `","role":"contributor"}`,
		},
		{name: "empty", body: displayNameBody(""), wantStatus: http.StatusBadRequest, wantBody: emptyNameBody},
		{name: "whitespace_only", body: displayNameBody("   "), wantStatus: http.StatusBadRequest,
			wantBody: emptyNameBody},
		{name: "too_long", body: displayNameBody(sixtyFour + "a"), wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid display name: must be at most 64 characters"}`},
		{name: "format_character", body: displayNameBody("Mor\u202ewen"), wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request",` +
				`"message":"invalid display name: must contain only letters, marks, numbers, punctuation, symbols and spaces, ` +
				`not U+202E"}`},
		{
			// A valid name padded past the raw limit, so only the raw check rejects it.
			name: "padded_past_raw_limit", body: displayNameBody(strings.Repeat(" ", 254) + "Bob"),
			invalidRequest: true, wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request",` +
				`"message":"invalid display name: must be at most 256 characters before normalisation"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo := &fakerepo.Repo{Users: map[string]domain.User{principalUserID: {
				ID: principalUserID, DisplayName: testDisplayName, Role: domain.RoleContributor, CreatedAt: testNow,
			}}}
			req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/v1/me", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req = asPrincipal(req)

			handler := MePatch{Command: &command.UpdateDisplayName{Users: repo}}
			validator := openapitest.New(t)
			var rec *httptest.ResponseRecorder
			if tc.invalidRequest {
				rec = validator.ServeResponseOnly(t, handler, req)
			} else {
				rec = validator.Serve(t, handler, req)
			}

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
