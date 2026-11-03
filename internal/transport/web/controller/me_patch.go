package controller

import (
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// MePatchRequest is the JSON body of a request to update the principal's user.
type MePatchRequest struct {
	DisplayName string `json:"display_name"`
}

// MePatch handles PATCH /v1/me, setting the principal's display name.
type MePatch struct {
	Command command.Command[command.UpdateDisplayNameRequest, domain.User]
}

func (c MePatch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	principal, ok := principalOrUnauthenticated(w, r)
	if !ok {
		return
	}

	var body MePatchRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}

	// A missing display_name decodes as empty, which the command rejects like any other empty name.
	user, err := c.Command.Execute(ctx, command.UpdateDisplayNameRequest{
		UserID:      principal.User.ID,
		DisplayName: body.DisplayName,
	})
	switch {
	case errors.Is(err, domain.ErrInvalidDisplayName):
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest, err.Error())
	case errors.Is(err, domain.ErrDisplayNameReserved):
		httpjson.WriteError(ctx, w, http.StatusUnprocessableEntity, httpjson.CodeDisplayNameReserved,
			"this display name is reserved")
	case errors.Is(err, datasources.ErrDisplayNameTaken):
		httpjson.WriteError(ctx, w, http.StatusConflict, httpjson.CodeDisplayNameTaken,
			"another user has this display name, or one that differs only in case, spaces or punctuation")
	case err != nil:
		writeInternalError(w, r, "unable to update display name", err)
	default:
		httpjson.WriteJSON(ctx, w, http.StatusOK, userFromDomain(user))
	}
}
