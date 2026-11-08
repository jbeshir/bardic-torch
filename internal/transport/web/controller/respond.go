package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
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

// unavailableMessage is the message of every 503 unavailable response to a database that could not serve the
// request.
const unavailableMessage = "the database is busy or cannot be reached; try again"

// WriteInternalError writes the response to err, an error no more specific response covers, keeping the details
// out of the response: 503 unavailable if err wraps datasources.ErrUnavailable (the database could not be reached,
// or a write could not finish in time), logged with msg as a warning, and 500 internal otherwise, logged as an
// error.
func WriteInternalError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	ctx := r.Context()
	logger := domain.LoggerFromContext(ctx)
	if errors.Is(err, datasources.ErrUnavailable) {
		logger.WarnContext(ctx, msg, "error", err)
		httpjson.WriteError(ctx, w, http.StatusServiceUnavailable, httpjson.CodeUnavailable, unavailableMessage)

		return
	}
	logger.ErrorContext(ctx, msg, "error", err)
	httpjson.WriteError(ctx, w, http.StatusInternalServerError, httpjson.CodeInternal, "internal error")
}

// maxSafeID is the largest ID: 2^53-1, the largest integer a JSON number holds exactly in every client (an IEEE
// double). The IDs are AUTO_INCREMENT columns, far below it.
const maxSafeID = 1<<53 - 1

// idOrBadRequest returns the request's path value name as an ID: an integer from 1 to maxSafeID in decimal
// digits. If it is not one it writes 400 invalid_request and returns false; the caller should then return
// without writing.
func idOrBadRequest(w http.ResponseWriter, r *http.Request, name string) (int64, bool) {
	id, ok := parseDecimal(r.PathValue(name), 1, maxSafeID)
	if !ok {
		httpjson.WriteError(r.Context(), w, http.StatusBadRequest, httpjson.CodeInvalidRequest,
			fmt.Sprintf("invalid %s: must be an integer from 1 to %d", name, int64(maxSafeID)))
		return 0, false
	}

	return id, true
}
