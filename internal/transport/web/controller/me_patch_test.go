package controller

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
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
			wantBody:   `{"id":"google-oauth2|123","public_id":"k3vq7xw2mbnc4zra","display_name":"Morwen of the Hills","role":"contributor"}`,
		},
		{
			name:       "padded_max_length",
			body:       displayNameBody("  " + sixtyFour + "  "),
			stored:     sixtyFour,
			wantStatus: http.StatusOK,
			wantBody:   `{"id":"google-oauth2|123","public_id":"k3vq7xw2mbnc4zra","display_name":"` + sixtyFour + `","role":"contributor"}`,
		},
		{
			// 256 characters as sent, the most the spec allows.
			name:       "padded_to_raw_limit",
			body:       displayNameBody(strings.Repeat(" ", 253) + "Bob"),
			stored:     "Bob",
			wantStatus: http.StatusOK,
			wantBody:   `{"id":"google-oauth2|123","public_id":"k3vq7xw2mbnc4zra","display_name":"Bob","role":"contributor"}`,
		},
		{
			// 128 code points as sent, 64 once composed.
			name:       "decomposed_max_length",
			body:       displayNameBody(strings.Repeat(decomposedE, domain.MaxDisplayNameLength)),
			stored:     strings.Repeat(composedE, domain.MaxDisplayNameLength),
			wantStatus: http.StatusOK,
			wantBody: `{"id":"google-oauth2|123","public_id":"k3vq7xw2mbnc4zra","display_name":"` +
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
				`"message":"invalid display name: must contain only letters, marks, numbers, punctuation, symbols and spaces, ` +
				`not U+202E"}`,
		},
		{
			name:       "taken",
			body:       displayNameBody("Mor-wen"),
			stored:     "Mor-wen",
			storeErr:   datasources.ErrDisplayNameTaken,
			wantStatus: http.StatusConflict,
			wantBody: `{"code":"display_name_taken","message":"another user has this display name, or one that ` +
				`differs only in case, spaces or punctuation"}`,
		},
		{
			name:       "reserved",
			body:       displayNameBody("Moderator"),
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   `{"code":"display_name_reserved","message":"this display name is reserved"}`,
		},
		{
			// A blank letter after a reserved name would make it pass for that name.
			name:       "blank_letter",
			body:       displayNameBody("Moderator\u3164"),
			wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid display name: must not contain a blank letter ` +
				`(a Hangul filler, the braille blank or a default-ignorable character)"}`,
		},
		{
			name:       "mixed_scripts",
			body:       displayNameBody("M\u043erwen"),
			wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request",` +
				`"message":"invalid display name: must not mix Latin, Cyrillic and Greek letters"}`,
		},
		{
			name:       "no_letter_or_digit",
			body:       displayNameBody("\U0001f3bb\U0001f3b5"),
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"invalid_request","message":"invalid display name: must contain a letter or a digit"}`,
		},
		{
			name:       "key_too_long",
			body:       displayNameBody(strings.Repeat("ﷺ", 18)),
			wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request",` +
				`"message":"invalid display name: must be at most 255 characters once folded for comparison"}`,
		},
		{
			name:       "contains_reserved",
			body:       displayNameBody("The Bardic Torch Crew"),
			wantStatus: http.StatusUnprocessableEntity,
			wantBody:   `{"code":"display_name_reserved","message":"this display name is reserved"}`,
		},
		{
			name:       "store_failure",
			body:       displayNameBody(testDisplayName),
			stored:     testDisplayName,
			storeErr:   errors.New("connection refused"),
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
		{
			name:       anonymousCase,
			body:       displayNameBody(testDisplayName),
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
			// A valid name padded past the raw limit, so only the raw check rejects it.
			name:           "padded_past_raw_limit",
			body:           displayNameBody(strings.Repeat(" ", 254) + "Bob"),
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody: `{"code":"invalid_request",` +
				`"message":"invalid display name: must be at most 256 characters before normalisation"}`,
		},
		{
			name:           "unknown_field",
			body:           `{"display_name":"Morwen","role":"moderator"}`,
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody:       `{"code":"invalid_request","message":"request body has an unknown field"}`,
		},
		{
			name:           "missing_body",
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody:       `{"code":"invalid_request","message":"request body must not be empty"}`,
		},
		{
			name:           tooLargeCase,
			body:           displayNameBody(strings.Repeat("a", httpjson.MaxBodyBytes)),
			invalidRequest: true,
			wantStatus:     http.StatusRequestEntityTooLarge,
			wantBody:       tooLargeBody,
		},
		{
			name:           notJSONCase,
			body:           `display_name=Morwen`,
			contentType:    "application/x-www-form-urlencoded",
			invalidRequest: true,
			wantStatus:     http.StatusUnsupportedMediaType,
			wantBody:       unsupportedMediaTypeBody,
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
						PublicID:    testPublicID,
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
