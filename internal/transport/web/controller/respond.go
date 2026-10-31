package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// principalOrUnauthenticated returns the request's principal. If the request is anonymous it writes 401
// unauthenticated and returns false; the caller should then return without writing. The router puts require*
// middleware in front of these controllers, so this only answers requests that reach one some other way.
func principalOrUnauthenticated(w http.ResponseWriter, r *http.Request) (domain.Principal, bool) {
	principal, ok := domain.PrincipalFromContext(r.Context())
	if !ok {
		httpjson.WriteError(r.Context(), w, http.StatusUnauthorized, httpjson.CodeUnauthenticated,
			"authentication required")
	}

	return principal, ok
}

// writeInternalError logs err with msg and writes 500 internal, keeping the details out of the response.
func writeInternalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	ctx := r.Context()
	domain.LoggerFromContext(ctx).ErrorContext(ctx, msg, "error", err)
	httpjson.WriteError(ctx, w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
}
