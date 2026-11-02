package router

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveUserFunc adapts a function to the user-resolving command the auth middleware takes.
type resolveUserFunc func(ctx context.Context, userID string) (domain.User, error)

func (f resolveUserFunc) Execute(ctx context.Context, userID string) (domain.User, error) {
	return f(ctx, userID)
}

func resolveTo(user domain.User) resolveUserFunc {
	return func(_ context.Context, userID string) (domain.User, error) {
		if userID != user.ID {
			return domain.User{}, fmt.Errorf("unexpected user ID [%s]", userID)
		}
		return user, nil
	}
}

func failIfResolved(t *testing.T) resolveUserFunc {
	return func(context.Context, string) (domain.User, error) {
		t.Error("user resolved unexpectedly")
		return domain.User{}, errors.New("unexpected")
	}
}

func notApplicable(*http.Request) (*Identity, error) {
	return nil, nil
}

func authenticatesAs(identity Identity) AuthValidator {
	return func(*http.Request) (*Identity, error) {
		return &identity, nil
	}
}

func failsWith(err error) AuthValidator {
	return func(*http.Request) (*Identity, error) {
		return nil, err
	}
}

func failIfCalled(t *testing.T) http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler called unexpectedly")
	})
}

var testUser = domain.User{ID: testUserID, DisplayName: "Morwen", Role: domain.RoleContributor, CreatedAt: testNow}

func TestAuthMiddleware_StoresPrincipal(t *testing.T) {
	t.Parallel()

	validators := []AuthValidator{
		notApplicable,
		authenticatesAs(Identity{UserID: testUserID, Method: domain.AuthMethodAPIToken}),
		failsWith(errors.New("validators after the first applicable one must not run")),
	}

	var got domain.Principal
	var authenticated bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, authenticated = domain.PrincipalFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	})

	rec := httptest.NewRecorder()
	authMiddleware(validators, resolveTo(testUser))(next).
		ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/tokens", nil))

	assert.Equal(t, http.StatusNoContent, rec.Code)
	require.True(t, authenticated)
	assert.Equal(t, domain.Principal{User: testUser, Method: domain.AuthMethodAPIToken}, got)
}

func TestAuthMiddleware_NoApplicableValidatorIsAnonymous(t *testing.T) {
	t.Parallel()

	for name, validators := range map[string][]AuthValidator{
		"no_validators":   nil,
		"none_applicable": {notApplicable, notApplicable},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var authenticated bool
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, authenticated = domain.PrincipalFromContext(r.Context())
				w.WriteHeader(http.StatusNoContent)
			})

			rec := httptest.NewRecorder()
			authMiddleware(validators, failIfResolved(t))(next).
				ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/tokens", nil))

			assert.Equal(t, http.StatusNoContent, rec.Code)
			assert.False(t, authenticated)
		})
	}
}

func TestAuthMiddleware_Rejections(t *testing.T) {
	t.Parallel()

	contributor := Identity{UserID: testUserID, Method: domain.AuthMethodAuth0}
	cases := []struct {
		name       string
		validator  AuthValidator
		resolve    func(t *testing.T) resolveUserFunc
		wantStatus int
		wantBody   string
	}{
		{
			name:       "invalid_credentials",
			validator:  failsWith(fmt.Errorf("%w: signature mismatch", ErrInvalidCredentials)),
			resolve:    failIfResolved,
			wantStatus: http.StatusUnauthorized,
			wantBody:   `{"code":"unauthenticated","message":"invalid credentials"}`,
		},
		{
			name:       "validator_failure",
			validator:  failsWith(errors.New("connection refused")),
			resolve:    failIfResolved,
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"code":"internal","message":"internal error"}`,
		},
		{
			name:      "resolver_failure",
			validator: authenticatesAs(contributor),
			resolve: func(*testing.T) resolveUserFunc {
				return func(context.Context, string) (domain.User, error) {
					return domain.User{}, errors.New("connection refused")
				}
			},
			wantStatus: http.StatusInternalServerError,
			wantBody:   `{"code":"internal","message":"internal error"}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			authMiddleware([]AuthValidator{tc.validator}, tc.resolve(t))(failIfCalled(t)).
				ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/v1/tokens", nil))

			assert.Equal(t, tc.wantStatus, rec.Code)
			assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			assert.JSONEq(t, tc.wantBody, rec.Body.String())
		})
	}
}

func TestAuthMiddleware_LogsUserID(t *testing.T) {
	t.Parallel()

	authenticated := authMiddleware(
		[]AuthValidator{authenticatesAs(Identity{UserID: testUserID, Method: domain.AuthMethodAuth0})},
		resolveTo(testUser),
	)
	anonymous := authMiddleware([]AuthValidator{notApplicable}, failIfResolved(t))
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		domain.LoggerFromContext(r.Context()).InfoContext(r.Context(), "handling")
		w.WriteHeader(http.StatusNoContent)
	})
	routes := []route{
		{method: http.MethodGet, path: "/authenticated", handler: authenticated(handler)},
		{method: http.MethodGet, path: "/anonymous", handler: anonymous(handler)},
	}

	_, entries := serveLogged(t, routes,
		httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/authenticated", nil))
	require.Len(t, entries, 2)
	for _, entry := range entries {
		assert.Equal(t, testUserID, entry["user_id"], entry["msg"])
	}

	_, entries = serveLogged(t, routes, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/anonymous", nil))
	require.Len(t, entries, 2)
	for _, entry := range entries {
		assert.NotContains(t, entry, "user_id", entry["msg"])
	}
}
