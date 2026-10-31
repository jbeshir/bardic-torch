package router

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// requireAuth lets only authenticated users who are not blocked through. Anonymous requests get 401
// unauthenticated and blocked users 403 user_blocked, so every route behind a require* middleware rejects
// blocked users.
func requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		principal, ok := domain.PrincipalFromContext(ctx)
		if !ok {
			httpjson.WriteError(ctx, w, http.StatusUnauthorized, httpjson.CodeUnauthenticated,
				"authentication required")
			return
		}

		if principal.User.IsBlocked() {
			httpjson.WriteError(ctx, w, http.StatusForbidden, httpjson.CodeUserBlocked, "user is blocked")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// requireNonAPIToken is requireAuth that also rejects requests authenticated with an API token, with 403
// api_token_not_allowed. Token management uses it, so that an API token cannot create or revoke tokens.
func requireNonAPIToken(next http.Handler) http.Handler {
	return requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// requireAuth has already checked that there is a principal.
		if principal, _ := domain.PrincipalFromContext(ctx); principal.Method == domain.AuthMethodAPIToken {
			httpjson.WriteError(ctx, w, http.StatusForbidden, httpjson.CodeAPIAuthNotAllowed,
				"this endpoint cannot be used with an API token")
			return
		}

		next.ServeHTTP(w, r)
	}))
}

// requireModerator is requireAuth that also rejects users who are not moderators, with 403 moderator_required.
// Moderators may use either credential.
func requireModerator(next http.Handler) http.Handler {
	return requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// requireAuth has already checked that there is a principal.
		if principal, _ := domain.PrincipalFromContext(ctx); principal.User.Role != domain.RoleModerator {
			httpjson.WriteError(ctx, w, http.StatusForbidden, httpjson.CodeModeratorRequired,
				"this endpoint requires the moderator role")
			return
		}

		next.ServeHTTP(w, r)
	}))
}
