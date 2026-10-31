package controller

import (
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// APITokenCreateRequest is the JSON body of a request to create a token.
type APITokenCreateRequest struct {
	Name string `json:"name"`
}

// CreatedAPIToken is the JSON response for a created token: the token plus its secret, which is shown only
// this once.
type CreatedAPIToken struct {
	APIToken
	Token string `json:"token"`
}

// APITokenCreate handles POST /v1/tokens, creating an API token for the principal.
type APITokenCreate struct {
	Command command.Command[command.CreateAPITokenRequest, command.CreateAPITokenResponse]
}

func (c APITokenCreate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}

	var body APITokenCreateRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}

	res, err := c.Command.Execute(ctx, command.CreateAPITokenRequest{UserID: principal.User.ID, Name: body.Name})
	switch {
	case errors.Is(err, command.ErrInvalidAPITokenName):
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest, err.Error())
	case errors.Is(err, command.ErrAPITokenLimitReached):
		httpjson.WriteError(ctx, w, http.StatusConflict, httpjson.CodeAPIAuthLimitReached, err.Error())
	case err != nil:
		writeInternalError(w, r, "unable to create API token", err)
	default:
		httpjson.WriteJSON(ctx, w, http.StatusCreated, CreatedAPIToken{
			APIToken: apiTokenFromDomain(res.Token),
			Token:    res.Secret,
		})
	}
}
