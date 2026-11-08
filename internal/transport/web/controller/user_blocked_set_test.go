package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/testutil/openapitest"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
)

// setUserBlockedFunc adapts a function to the command UserBlockedSet takes.
type setUserBlockedFunc func(ctx context.Context, req command.SetUserBlockedRequest) (domain.User, error)

func (f setUserBlockedFunc) Execute(ctx context.Context, req command.SetUserBlockedRequest) (domain.User, error) {
	return f(ctx, req)
}

func TestUserBlockedSet_ServeHTTP(t *testing.T) {
	t.Parallel()

	const (
		targetID = "google-oauth2|456"
		// encodedTargetID is targetID as it appears in the path; the router decodes it once for the controller.
		encodedTargetID = "google-oauth2%7C456"
		targetBody      = `{"id":"google-oauth2|456","display_name":"Morwen","role":"contributor"}`
		blockBody       = `{"blocked":true}`
	)
	target := domain.User{ID: targetID, DisplayName: testDisplayName, Role: domain.RoleContributor, CreatedAt: testNow}
	moderator := domain.Principal{User: domain.User{ID: principalUserID, DisplayName: testDisplayName,
		Role: domain.RoleModerator, CreatedAt: testNow}, Method: domain.AuthMethodAuth0}

	cases := []struct {
		name        string
		body        string
		contentType string
		// invalidRequest marks requests that deliberately break the request schema, so only the response is
		// checked against the spec.
		invalidRequest bool
		anonymous      bool
		// wantBlocked is what the command should be asked to set; nil means it is not called.
		wantBlocked *bool
		user        domain.User
		err         error
		wantStatus  int
		wantBody    string
	}{
		{
			name:        "blocked",
			body:        blockBody,
			wantBlocked: new(true),
			user:        target,
			wantStatus:  http.StatusOK,
			wantBody:    targetBody,
		},
		{
			name:        "unblocked",
			body:        `{"blocked":false}`,
			wantBlocked: new(false),
			user:        target,
			wantStatus:  http.StatusOK,
			wantBody:    targetBody,
		},
		{
			name:        "unknown_user",
			body:        blockBody,
			wantBlocked: new(true),
			err:         datasources.ErrNotFound,
			wantStatus:  http.StatusNotFound,
			wantBody:    `{"code":"not_found","message":"user not found"}`,
		},
		{
			name:        "moderator",
			body:        blockBody,
			wantBlocked: new(true),
			err:         command.ErrModeratorNotBlockable,
			wantStatus:  http.StatusConflict,
			wantBody:    `{"code":"conflict","message":"moderators cannot be blocked or unblocked"}`,
		},
		{
			name:        "not_interactive_moderator",
			body:        blockBody,
			wantBlocked: new(true),
			err:         domain.ErrModeratorRequired,
			wantStatus:  http.StatusForbidden,
			wantBody:    moderatorRequiredBy,
		},
		{
			name:       anonymousCase,
			body:       blockBody,
			anonymous:  true,
			wantStatus: http.StatusUnauthorized,
			wantBody:   unauthenticatedBody,
		},
		{
			name:        "command_failure",
			body:        `{"blocked":false}`,
			wantBlocked: new(false),
			err:         errors.New("connection refused"),
			wantStatus:  http.StatusInternalServerError,
			wantBody:    internalErrorBody,
		},
		{
			name:           "missing_blocked",
			body:           `{}`,
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody:       `{"code":"invalid_request","message":"blocked is required"}`,
		},
		{
			name:           "null_blocked",
			body:           `{"blocked":null}`,
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody:       `{"code":"invalid_request","message":"blocked is required"}`,
		},
		{
			name:           "blocked_not_boolean",
			body:           `{"blocked":"yes"}`,
			invalidRequest: true,
			wantStatus:     http.StatusBadRequest,
			wantBody:       `{"code":"invalid_request","message":"field blocked has the wrong type"}`,
		},
		{
			name:           tooLargeCase,
			body:           `{"blocked":true,"padding":"` + strings.Repeat("a", httpjson.MaxBodyBytes) + `"}`,
			invalidRequest: true,
			wantStatus:     http.StatusRequestEntityTooLarge,
			wantBody:       tooLargeBody,
		},
		{
			name:           notJSONCase,
			body:           `blocked=true`,
			contentType:    "text/plain",
			invalidRequest: true,
			wantStatus:     http.StatusUnsupportedMediaType,
			wantBody:       unsupportedMediaTypeBody,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := UserBlockedSet{Command: setUserBlockedFunc(
				func(_ context.Context, req command.SetUserBlockedRequest) (domain.User, error) {
					if tc.wantBlocked == nil {
						t.Error("command called unexpectedly")
					} else {
						assert.Equal(t, command.SetUserBlockedRequest{Principal: moderator, UserID: targetID,
							Blocked: *tc.wantBlocked}, req)
					}
					return tc.user, tc.err
				},
			)}

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/v1/users/"+encodedTargetID+"/blocked",
				strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			req.SetPathValue("user_id", targetID)
			if !tc.anonymous {
				req = req.WithContext(domain.ContextWithPrincipal(req.Context(), moderator))
			}

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
