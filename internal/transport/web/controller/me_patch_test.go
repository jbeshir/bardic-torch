package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources/mocks"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

const emptyNameBody = `{"code":"invalid_request","message":"invalid display name: must not be empty"}`

func displayNameBody(name string) string {
	return `{"display_name":"` + name + `"}`
}

func TestMePatch_ServeHTTP(t *testing.T) {
	t.Parallel()

	// composedE is "\u00e9" as one code point; decomposedE is "e" followed by a combining acute accent, which NFC
	// composes into composedE.
	const (
		composedE   = "\u00e9"
		decomposedE = "e\u0301"
	)
	sixtyFour := strings.Repeat("a", domain.MaxDisplayNameLength)

	cases := []struct {
		name        string
		body        string
		contentType string
		anonymous   bool
		// invalidRequest marks requests that deliberately break the request schema, so only the response is
		// checked against the spec.
		invalidRequest bool
		// stored is the name the datasource should be given; empty means it is not called.
		stored     string
		storeErr   error
		wantStatus int
		wantBody   string
	}{
		{
			name:       "renamed",
			body:       displayNameBody("Morwen of the Hills"),
			stored:     "Morwen of the Hills",
			wantStatus: http.StatusOK,
			wantBody:   `{"id":"google-oauth2|123","display_name":"Morwen of the Hills","role":"contributor"}`,
		},
		{
			name:       "padded_max_length",
			body:       displayNameBody("  " + sixtyFour + "  "),
			stored:     sixtyFour,
			wantStatus: http.StatusOK,
			wantBody:   `{"id":"google-oauth2|123","display_name":"` + sixtyFour + `","role":"contributor"}`,
		},
		{
			// 128 code points as sent, 64 once composed.
			name:       "decomposed_max_length",
			body:       displayNameBody(strings.Repeat(decomposedE, domain.MaxDisplayNameLength)),
			stored:     strings.Repeat(composedE, domain.MaxDisplayNameLength),
			wantStatus: http.StatusOK,
			wantBody: `{"id":"google-oauth2|123","display_name":"` +
				strings.Repeat(composedE, domain.MaxDisplayNameLength) + `","role":"contributor"}`,
		},
		{
			name:       "empty",
			body:       displayNameBody(""),
			wantStatus: http.StatusBadRequest,
			wantBody:   emptyNameBody,
		},
		{
			name:       "whitespace_only",
			body:       displayNameBody("   "),
			wantStatus: http.StatusBadRequest,
			wantBody:   emptyNameBody,
		},
		{
			name:       "too_long",
			body:       displayNameBody(sixtyFour + "a"),
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"invalid_request","message":"invalid display name: must be at most 64 characters"}`,
		},
		{
			name:       "format_character",
			body:       displayNameBody("Mor\u202ewen"),
			wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request",` +
				`"message":"invalid display name: must not contain control or format characters"}`,
		},
		{
			name:       "store_failure",
			body:       displayNameBody("Morwen"),
			stored:     "Morwen",
			storeErr:   errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
		{
			name:       anonymousCase,
			body:       displayNameBody("Morwen"),
			anonymous:  true,
			wantStatus: http.StatusUnauthorized,
			wantBody:   unauthenticatedBody,
		},
		{
			name:           "missing_display_name",
			body:           `{}`,
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody:       emptyNameBody,
		},
		{
			name:           "null_display_name",
			body:           `{"display_name":null}`,
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody:       emptyNameBody,
		},
		{
			// Longer than the raw limit in the spec, which the server enforces after normalising instead.
			name:           "longer_than_raw_limit",
			body:           displayNameBody(strings.Repeat("a", 257)),
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody:       `{"code":"invalid_request","message":"invalid display name: must be at most 64 characters"}`,
		},
		{
			name:           "unknown_field",
			body:           `{"display_name":"Morwen","role":"moderator"}`,
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody:       `{"code":"invalid_request","message":"invalid request body: json: unknown field \"role\""}`,
		},
		{
			name:           "missing_body",
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody:       `{"code":"invalid_request","message":"request body must not be empty"}`,
		},
		{
			name:           "too_large",
			body:           displayNameBody(strings.Repeat("a", httpjson.MaxBodyBytes)),
			invalidRequest: true,
			wantStatus:     http.StatusRequestEntityTooLarge,
			wantBody:       `{"code":"request_too_large","message":"request body is too large"}`,
		},
		{
			name:           "not_json",
			body:           `display_name=Morwen`,
			contentType:    "application/x-www-form-urlencoded",
			invalidRequest: true,
			wantStatus:     http.StatusUnsupportedMediaType,
			wantBody:       `{"code":"unsupported_media_type","message":"request body must be sent as application/json"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mocks fail the test if they are called when tc.stored is empty.
			updater := mocks.NewDisplayNameUpdater(t)
			getter := mocks.NewUserGetter(t)
			if tc.stored != "" {
				updater.EXPECT().UpdateDisplayName(mock.Anything, principalUserID, tc.stored).Return(tc.storeErr)
				if tc.storeErr == nil {
					getter.EXPECT().GetUser(mock.Anything, principalUserID).Return(domain.User{
						ID:          principalUserID,
						DisplayName: tc.stored,
						Role:        domain.RoleContributor,
						CreatedAt:   testNow,
					}, nil)
				}
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPatch, "/v1/me", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			if !tc.anonymous {
				req = asPrincipal(req)
			}

			handler := MePatch{Command: &command.UpdateDisplayName{Updater: updater, Getter: getter}}
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
