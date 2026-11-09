package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestNewHTTPHandler_UserBlockedRouteTargets(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		path       string
		body       string
		targetID   string
		expect     func(m handlerMocks, targetID string)
		wantStatus int
		wantBody   string
	}{
		{
			name:     "encoded_id_blocked",
			path:     "/v1/users/google-oauth2%7C123/blocked",
			body:     blockBody,
			targetID: blockTargetID,
			expect: func(m handlerMocks, targetID string) {
				m.blocker.EXPECT().BlockUser(mock.Anything, moderatorID, targetID, handlerNow).Return(nil)
				m.users.EXPECT().GetUser(mock.Anything, targetID).Return(storedUser(targetID), nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"id":"google-oauth2|123","public_id":"k3vq7xw2mbnc4zra","display_name":"Morwen","role":"contributor"}`,
		},
		{
			// Clients that send "|" unencoded reach the same user.
			name:     "unencoded_id_unblocked",
			path:     "/v1/users/google-oauth2|123/blocked",
			body:     `{"blocked":false}`,
			targetID: blockTargetID,
			expect: func(m handlerMocks, targetID string) {
				m.unblocker.EXPECT().UnblockUser(mock.Anything, moderatorID, targetID).Return(nil)
				m.users.EXPECT().GetUser(mock.Anything, targetID).Return(storedUser(targetID), nil)
			},
			wantStatus: http.StatusOK,
			wantBody:   `{"id":"google-oauth2|123","public_id":"k3vq7xw2mbnc4zra","display_name":"Morwen","role":"contributor"}`,
		},
		{
			// An encoded "/" stays inside the ID rather than splitting the path.
			name:     "encoded_slash_stays_in_id",
			path:     "/v1/users/auth0%7Ca%2Fb/blocked",
			body:     blockBody,
			targetID: "auth0|a/b",
			expect: func(m handlerMocks, targetID string) {
				m.blocker.EXPECT().BlockUser(mock.Anything, moderatorID, targetID, handlerNow).Return(datasources.ErrNotFound)
				m.users.EXPECT().GetUser(mock.Anything, targetID).Return(domain.User{}, datasources.ErrNotFound)
			},
			wantStatus: http.StatusNotFound,
			wantBody:   `{"code":"not_found","message":"user not found"}`,
		},
		{
			name:     "moderator_self",
			path:     "/v1/users/google-oauth2%7Cmoderator/blocked",
			body:     blockBody,
			targetID: moderatorID,
			expect: func(m handlerMocks, targetID string) {
				m.blocker.EXPECT().BlockUser(mock.Anything, moderatorID, targetID, handlerNow).Return(datasources.ErrNotFound)
				m.users.EXPECT().GetUser(mock.Anything, targetID).Return(storedUser(targetID), nil).Once()
			},
			wantStatus: http.StatusConflict,
			wantBody:   `{"code":"conflict","message":"moderators cannot be blocked or unblocked"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m := newHandlerMocks(t)
			m.expectUser(moderatorID)
			tc.expect(m, tc.targetID)

			req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer auth0|"+moderatorID)
			rec := m.validate.Serve(t, m.handler, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}
