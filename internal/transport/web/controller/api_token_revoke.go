package controller

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
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

	err := c.Revoker.RevokeAPIToken(ctx, principal.User.ID, mux.Vars(r)["token_id"], c.Now())
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, "API token not found")
	case err != nil:
		writeInternalError(w, r, "unable to revoke API token", err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}
