package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// MeGet handles GET /v1/me, returning the principal's user. The auth middleware has already resolved the
// user, so this reads nothing more.
type MeGet struct{}

func (MeGet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}

	httpjson.WriteJSON(r.Context(), w, http.StatusOK, userFromDomain(principal.User))
}
