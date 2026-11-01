package controller

import (
	"fmt"
	"math"
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

// idOrBadRequest returns the request's path value name as an ID: an integer from 1 to math.MaxInt64 in
// decimal digits. If it is not one it writes 400 invalid_request and returns false; the caller should then return
// without writing.
func idOrBadRequest(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, ok := parseDecimal(r.PathValue(name), 1, math.MaxInt64)
	if !ok {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			fmt.Sprintf("invalid %s: must be an integer from 1 to %d", name, int64(math.MaxInt64)))
		return 0, false
	}

	return id, true
}
