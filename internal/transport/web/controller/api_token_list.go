package controller

import (
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// APITokenListResponse is the JSON response listing a user's tokens.
type APITokenListResponse struct {
	Data []APIToken `json:"data"`
}

// APITokenList handles GET /v1/tokens, listing the principal's active tokens, newest first.
type APITokenList struct {
	Lister datasources.ActiveAPITokenLister
}

func (c APITokenList) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}

	tokens, err := c.Lister.ListUserActiveAPITokens(ctx, principal.User.ID)
	if err != nil {
		writeInternalError(w, r, "unable to list API tokens", err)
		return
	}

	list := APITokenListResponse{Data: make([]APIToken, 0, len(tokens))}
	for _, token := range tokens {
		list.Data = append(list.Data, apiTokenFromDomain(token))
	}

	httpjson.WriteJSON(ctx, w, http.StatusOK, list)
}
