package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// ErrModeratorNotBlockable is returned when the user to block or unblock is a moderator.
var ErrModeratorNotBlockable = errors.New("moderators cannot be blocked or unblocked")

var _ Command[SetUserBlockedRequest, domain.User] = (*SetUserBlocked)(nil)

// SetUserBlockedRequest is the request for the SetUserBlocked command.
type SetUserBlockedRequest struct {
	UserID  string
	Blocked bool
}

// SetUserBlocked blocks or unblocks a contributor.
type SetUserBlocked struct {
	Blocker   datasources.UserBlocker
	Unblocker datasources.UserUnblocker
	Getter    datasources.UserGetter
	Now       func() time.Time
}

// Execute blocks or unblocks req.UserID and returns the user. Blocking a blocked user keeps the time they were
// first blocked. An unknown user returns an error wrapping datasources.ErrNotFound, and a moderator, including
// the caller, returns ErrModeratorNotBlockable.
func (c *SetUserBlocked) Execute(ctx context.Context, req SetUserBlockedRequest) (domain.User, error) {
	matched, err := c.setBlocked(ctx, req)
	if err != nil {
		return domain.User{}, err
	}

	user, err := c.Getter.GetUser(ctx, req.UserID)
	if err != nil {
		return domain.User{}, fmt.Errorf("getting user: %w", err)
	}
	// Only contributors match, so a user who exists but did not match is a moderator.
	if !matched {
		return domain.User{}, ErrModeratorNotBlockable
	}

	return user, nil
}

// setBlocked blocks or unblocks req.UserID, reporting whether a contributor with that ID matched.
func (c *SetUserBlocked) setBlocked(ctx context.Context, req SetUserBlockedRequest) (bool, error) {
	var err error
	if req.Blocked {
		err = c.Blocker.BlockUser(ctx, req.UserID, c.Now())
	} else {
		err = c.Unblocker.UnblockUser(ctx, req.UserID)
	}

	switch {
	case errors.Is(err, datasources.ErrNotFound):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("setting user blocked: %w", err)
	default:
		return true, nil
	}
}
