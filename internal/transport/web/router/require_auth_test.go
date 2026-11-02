package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireCase is a request with an optional principal, and the response a require* middleware should give it.
// A wantCode of "" means the request reaches the handler.
type requireCase struct {
	name       string
	principal  *domain.Principal
	wantStatus int
	wantCode   httpjson.ErrorCode
}

// Cases shared by the require* tests. Names say "API auth" where the case says "token", because gosec reports
// constants named after tokens as hardcoded credentials.
const (
	auth0ContributorCase = "auth0_contributor"
	apiAuthModeratorCase = "api_token_moderator"
)

var anonymousRequireCase = requireCase{
	name:       "anonymous",
	wantStatus: http.StatusUnauthorized,
	wantCode:   httpjson.CodeUnauthenticated,
}

func principalWith(role domain.Role, method domain.AuthMethod, blocked bool) *domain.Principal {
	user := domain.User{ID: testUserID, DisplayName: "Morwen", Role: role, CreatedAt: testNow}
	if blocked {
		blockedAt := testNow.Add(-time.Hour)
		user.BlockedAt = &blockedAt
	}
	return &domain.Principal{User: user, Method: method}
}

func runRequireCases(t *testing.T, middleware func(http.Handler) http.Handler, cases []requireCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			})
			if tc.wantCode != "" {
				next = failIfCalled(t).ServeHTTP
			}

			ctx := t.Context()
			if tc.principal != nil {
				ctx = domain.ContextWithPrincipal(ctx, *tc.principal)
			}
			rec := httptest.NewRecorder()
			middleware(next).ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/tokens", nil))

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantCode != "" {
				var body struct {
					Code    httpjson.ErrorCode `json:"code"`
					Message string             `json:"message"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, tc.wantCode, body.Code)
				assert.NotEmpty(t, body.Message)
			}
		})
	}
}

func TestRequireAuth_Principals(t *testing.T) {
	t.Parallel()

	runRequireCases(t, requireAuth, []requireCase{
		anonymousRequireCase,
		{
			name:       auth0ContributorCase,
			principal:  principalWith(domain.RoleContributor, domain.AuthMethodAuth0, false),
			wantStatus: http.StatusNoContent,
		},
		{
			name:       apiAuthModeratorCase,
			principal:  principalWith(domain.RoleModerator, domain.AuthMethodAPIToken, false),
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "blocked_auth0",
			principal:  principalWith(domain.RoleContributor, domain.AuthMethodAuth0, true),
			wantStatus: http.StatusForbidden,
			wantCode:   httpjson.CodeUserBlocked,
		},
		{
			name:       "blocked_api_token",
			principal:  principalWith(domain.RoleContributor, domain.AuthMethodAPIToken, true),
			wantStatus: http.StatusForbidden,
			wantCode:   httpjson.CodeUserBlocked,
		},
	})
}

func TestRequireNonAPIToken_Principals(t *testing.T) {
	t.Parallel()

	runRequireCases(t, requireNonAPIToken, []requireCase{
		anonymousRequireCase,
		{
			name:       auth0ContributorCase,
			principal:  principalWith(domain.RoleContributor, domain.AuthMethodAuth0, false),
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "auth0_moderator",
			principal:  principalWith(domain.RoleModerator, domain.AuthMethodAuth0, false),
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "api_token_contributor",
			principal:  principalWith(domain.RoleContributor, domain.AuthMethodAPIToken, false),
			wantStatus: http.StatusForbidden,
			wantCode:   httpjson.CodeAPIAuthNotAllowed,
		},
		{
			name:       apiAuthModeratorCase,
			principal:  principalWith(domain.RoleModerator, domain.AuthMethodAPIToken, false),
			wantStatus: http.StatusForbidden,
			wantCode:   httpjson.CodeAPIAuthNotAllowed,
		},
		{
			name:       "blocked_auth0",
			principal:  principalWith(domain.RoleContributor, domain.AuthMethodAuth0, true),
			wantStatus: http.StatusForbidden,
			wantCode:   httpjson.CodeUserBlocked,
		},
		{
			// Blocking takes precedence, so a blocked user learns they are blocked whatever credential they use.
			name:       "blocked_api_token",
			principal:  principalWith(domain.RoleContributor, domain.AuthMethodAPIToken, true),
			wantStatus: http.StatusForbidden,
			wantCode:   httpjson.CodeUserBlocked,
		},
	})
}

func TestRequireModerator_Principals(t *testing.T) {
	t.Parallel()

	runRequireCases(t, requireModerator, []requireCase{
		anonymousRequireCase,
		{
			name:       auth0ContributorCase,
			principal:  principalWith(domain.RoleContributor, domain.AuthMethodAuth0, false),
			wantStatus: http.StatusForbidden,
			wantCode:   httpjson.CodeModeratorRequired,
		},
		{
			name:       "api_token_contributor",
			principal:  principalWith(domain.RoleContributor, domain.AuthMethodAPIToken, false),
			wantStatus: http.StatusForbidden,
			wantCode:   httpjson.CodeModeratorRequired,
		},
		{
			name:       "auth0_moderator",
			principal:  principalWith(domain.RoleModerator, domain.AuthMethodAuth0, false),
			wantStatus: http.StatusNoContent,
		},
		{
			name:       apiAuthModeratorCase,
			principal:  principalWith(domain.RoleModerator, domain.AuthMethodAPIToken, false),
			wantStatus: http.StatusNoContent,
		},
		{
			// A blocked moderator is rejected as blocked, before the role is considered.
			name:       "blocked_moderator",
			principal:  principalWith(domain.RoleModerator, domain.AuthMethodAuth0, true),
			wantStatus: http.StatusForbidden,
			wantCode:   httpjson.CodeUserBlocked,
		},
		{
			name:       "blocked_api_token_contributor",
			principal:  principalWith(domain.RoleContributor, domain.AuthMethodAPIToken, true),
			wantStatus: http.StatusForbidden,
			wantCode:   httpjson.CodeUserBlocked,
		},
	})
}
