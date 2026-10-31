package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// APITokenRevoke handles DELETE /v1/tokens/{token_id}, revoking one of the principal's tokens. Revoking an
// already revoked token succeeds again; another user's token is not found.
type APITokenRevoke struct {
	Revoker datasources.APITokenRevoker
	Now     func() time.Time
}

func (c APITokenRevoke) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}

	tokenID, ok := pathVarOrBadRequest(w, r, "token_id")
	if !ok {
		return
	}

	// Token IDs are UUIDs in canonical form, so any other ID names no token. Rejecting it here, before the lookup,
	// keeps matching exact whatever the ID column's collation makes of trailing spaces or case.
	if parsed, err := uuid.Parse(tokenID); err != nil || parsed.String() != tokenID {
		writeAPITokenNotFound(w, r)
		return
	}

	err := c.Revoker.RevokeAPIToken(ctx, principal.User.ID, tokenID, c.Now())
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		writeAPITokenNotFound(w, r)
	case err != nil:
		writeInternalError(w, r, "unable to revoke API token", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeAPITokenNotFound(w http.ResponseWriter, r *http.Request) {
	httpjson.WriteError(r.Context(), w, http.StatusNotFound, httpjson.CodeNotFound, "API token not found")
}
