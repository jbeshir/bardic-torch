package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	"github.com/stretchr/testify/require"
)

// createAPITokenFunc adapts a function to the command APITokenCreate takes.
type createAPITokenFunc func(ctx context.Context, req command.CreateAPITokenRequest) (
	command.CreateAPITokenResponse, error)

func (f createAPITokenFunc) Execute(
	ctx context.Context, req command.CreateAPITokenRequest,
) (command.CreateAPITokenResponse, error) {
	return f(ctx, req)
}

const createdSecret = "user_api|0123abcd0123abcd0123abcd0123abcd0123abcd0123abcd0123abcd0123abcd"

// createReturning is a command that expects wantName from principalUserID and returns res and err.
func createReturning(
	t *testing.T, wantName string, res command.CreateAPITokenResponse, err error,
) createAPITokenFunc {
	return func(_ context.Context, req command.CreateAPITokenRequest) (command.CreateAPITokenResponse, error) {
		assert.Equal(t, command.CreateAPITokenRequest{UserID: principalUserID, Name: wantName}, req)
		return res, err
	}
}

func createdToken(name *string) command.CreateAPITokenResponse {
	return command.CreateAPITokenResponse{
		Token: domain.APIToken{
			ID:        "6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55",
			UserID:    principalUserID,
			Prefix:    "0123abcd",
			Name:      name,
			CreatedAt: testNow,
		},
		Secret: createdSecret,
	}
}

func notCalled(t *testing.T) createAPITokenFunc {
	return func(context.Context, command.CreateAPITokenRequest) (command.CreateAPITokenResponse, error) {
		t.Error("command called unexpectedly")
		return command.CreateAPITokenResponse{}, errors.New("unexpected")
	}
}

func TestAPITokenCreate_ServeHTTP(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		body        string
		contentType string
		anonymous   bool
		// invalidRequest marks requests that deliberately break the request schema, so only the response is
		// checked against the spec.
		invalidRequest bool
		command        func(t *testing.T) createAPITokenFunc
		wantStatus     int
		wantBody       string
	}{
		{
			name: "created_with_name",
			body: `{"name":"  CI runner "}`,
			command: func(t *testing.T) createAPITokenFunc {
				return createReturning(t, "  CI runner ", createdToken(new("CI runner")), nil)
			},
			wantStatus: http.StatusCreated,
			wantBody: `{"id":"6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55","prefix":"0123abcd","name":"CI runner",` +
				`"created_at":"2026-10-31T12:00:00Z","last_used_at":null,"token":"` + createdSecret + `"}`,
		},
		{
			name: "created_without_name",
			body: `{}`,
			command: func(t *testing.T) createAPITokenFunc {
				return createReturning(t, "", createdToken(nil), nil)
			},
			wantStatus: http.StatusCreated,
			wantBody: `{"id":"6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55","prefix":"0123abcd","name":null,` +
				`"created_at":"2026-10-31T12:00:00Z","last_used_at":null,"token":"` + createdSecret + `"}`,
		},
		{
			name: "null_name",
			body: `{"name":null}`,
			command: func(t *testing.T) createAPITokenFunc {
				return createReturning(t, "", createdToken(nil), nil)
			},
			wantStatus: http.StatusCreated,
			wantBody: `{"id":"6f1c1f0e-8a4b-4f43-9a5e-2d7c0b1e3a55","prefix":"0123abcd","name":null,` +
				`"created_at":"2026-10-31T12:00:00Z","last_used_at":null,"token":"` + createdSecret + `"}`,
		},
		{
			name: "name_too_long",
			body: `{"name":"` + strings.Repeat("a", 129) + `"}`,
			command: func(t *testing.T) createAPITokenFunc {
				return createReturning(t, strings.Repeat("a", 129), command.CreateAPITokenResponse{},
					fmt.Errorf("%w: must be at most 128 characters", command.ErrInvalidAPITokenName))
			},
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"invalid_request","message":"invalid API token name: must be at most 128 characters"}`,
		},
		{
			name: "limit_reached",
			body: `{}`,
			command: func(t *testing.T) createAPITokenFunc {
				return createReturning(t, "", command.CreateAPITokenResponse{}, command.ErrAPITokenLimitReached)
			},
			wantStatus: http.StatusConflict,
			wantBody: `{"code":"token_limit_reached",` +
				`"message":"user has reached the maximum number of active API tokens"}`,
		},
		{
			name: "command_failure",
			body: `{}`,
			command: func(t *testing.T) createAPITokenFunc {
				return createReturning(t, "", command.CreateAPITokenResponse{}, errors.New("connection refused"))
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   internalErrorBody,
		},
		{
			name:       anonymousCase,
			body:       `{}`,
			anonymous:  true,
			command:    notCalled,
			wantStatus: http.StatusUnauthorized,
			wantBody:   unauthenticatedBody,
		},
		{
			name:           "unknown_field",
			body:           `{"label":"CI runner"}`,
			invalidRequest: true,
			command:        notCalled,
			wantStatus:     http.StatusBadRequest,
			wantBody:       `{"code":"invalid_request","message":"request body has an unknown field"}`,
		},
		{
			name:           "missing_body",
			invalidRequest: true,
			command:        notCalled,
			wantStatus:     http.StatusBadRequest,
			wantBody:       `{"code":"invalid_request","message":"request body must not be empty"}`,
		},
		{
			name:           tooLargeCase,
			body:           `{"name":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `"}`,
			invalidRequest: true,
			command:        notCalled,
			wantStatus:     http.StatusRequestEntityTooLarge,
			wantBody:       tooLargeBody,
		},
		{
			name:           notJSONCase,
			body:           `name=CI+runner`,
			contentType:    "application/x-www-form-urlencoded",
			invalidRequest: true,
			command:        notCalled,
			wantStatus:     http.StatusUnsupportedMediaType,
			wantBody:       unsupportedMediaTypeBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/tokens", strings.NewReader(tc.body))
			if tc.contentType == "" {
				req.Header.Set("Content-Type", "application/json")
			} else {
				req.Header.Set("Content-Type", tc.contentType)
			}
			if !tc.anonymous {
				req = asPrincipal(req)
			}

			handler := APITokenCreate{Command: tc.command(t)}
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

// TestAPITokenCreate_NameRulesWithCommand checks the name rules of the real command through the handler, so that
// the spec's limits are tested against what the server actually does.
func TestAPITokenCreate_NameRulesWithCommand(t *testing.T) {
	t.Parallel()

	atLimit := strings.Repeat("\u00e9", 128)
	cases := []struct {
		name string
		// rawName is sent as the token name.
		rawName string
		// stored is the name the token should be created with; empty means nothing is stored.
		stored     string
		wantStatus int
		wantBody   string
	}{
		{name: "name_at_limit", rawName: atLimit, stored: atLimit, wantStatus: http.StatusCreated},
		{
			name:       "name_over_limit",
			rawName:    atLimit + "\u00e9",
			wantStatus: http.StatusBadRequest,
			wantBody:   `{"code":"invalid_request","message":"invalid API token name: must be at most 128 characters"}`,
		},
		{
			name:       "control_character",
			rawName:    "CI\\u001brunner",
			wantStatus: http.StatusBadRequest,
			wantBody: `{"code":"invalid_request","message":"invalid API token name: must contain only letters, ` +
				`marks, numbers, punctuation, symbols and spaces, not U+001B"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The mocks fail the test if they are called when tc.stored is empty.
			counter := mocks.NewActiveAPITokenCounter(t)
			creator := mocks.NewAPITokenCreator(t)
			if tc.stored != "" {
				counter.EXPECT().CountUserActiveAPITokens(mock.Anything, principalUserID).Return(0, nil)
				creator.EXPECT().CreateAPIToken(mock.Anything, mock.MatchedBy(func(token datasources.NewAPIToken) bool {
					return token.Name != nil && *token.Name == tc.stored
				})).Return(nil)
			}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/v1/tokens",
				strings.NewReader(`{"name":"`+tc.rawName+`"}`))
			req.Header.Set("Content-Type", "application/json")
			handler := APITokenCreate{Command: &command.CreateAPIToken{
				Counter: counter,
				Creator: creator,
				Now:     testClock,
			}}
			rec := openapitest.New(t).Serve(t, handler, asPrincipal(req))

			require.Equal(t, tc.wantStatus, rec.Code, rec.Body.String())
			if tc.stored == "" {
				assert.JSONEq(t, tc.wantBody, rec.Body.String())
				return
			}
			var created CreatedAPIToken
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))
			assert.Equal(t, &tc.stored, created.Name)
		})
	}
}
