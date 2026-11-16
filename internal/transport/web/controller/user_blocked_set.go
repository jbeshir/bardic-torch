package controller

import (
	"errors"
	"net/http"

	"github.com/jbeshir/bardic-torch/internal/command"
	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
	"github.com/jbeshir/bardic-torch/internal/transport/web/httpjson"
)

// UserBlockedSetRequest is the JSON body of a request to block or unblock a user. Blocked is a pointer so that
// a missing value is rejected rather than read as false, which would unblock the user.
type UserBlockedSetRequest struct {
	Blocked *bool `json:"blocked"`
}

// UserBlockedSet handles PUT /v1/users/{user_id}/blocked, blocking or unblocking a contributor.
type UserBlockedSet struct {
	Command command.Command[command.SetUserBlockedRequest, domain.User]
}

func (c UserBlockedSet) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var body UserBlockedSetRequest
	if !httpjson.DecodeJSONBody(w, r, &body) {
		return
	}
	if body.Blocked == nil {
		httpjson.WriteError(ctx, w, http.StatusBadRequest, httpjson.CodeInvalidRequest, "blocked is required")
		return
	}

	userID := r.PathValue("user_id")
	user, err := c.Command.Execute(ctx, command.SetUserBlockedRequest{UserID: userID, Blocked: *body.Blocked})
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		httpjson.WriteError(ctx, w, http.StatusNotFound, httpjson.CodeNotFound, "user not found")
	case errors.Is(err, command.ErrModeratorNotBlockable):
		httpjson.WriteError(ctx, w, http.StatusConflict, httpjson.CodeConflict, err.Error())
	case err != nil:
		writeInternalError(w, r, "unable to set user blocked", err)
	default:
		domain.LoggerFromContext(ctx).InfoContext(ctx, "set user blocked",
			"target_user_id", user.ID, "blocked", *body.Blocked)
		httpjson.WriteJSON(ctx, w, http.StatusOK, userFromDomain(user))
	}
}
