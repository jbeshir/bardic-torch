package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
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
