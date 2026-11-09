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

// errContributorNotMatched is returned if a contributor still does not match the update when it is retried, which
// only happens if their row changes between every statement.
var errContributorNotMatched = errors.New("contributor did not match the blocked update")

var _ Command[SetUserBlockedRequest, domain.User] = (*SetUserBlocked)(nil)

// SetUserBlockedRequest is Principal's request to block or unblock the user UserID.
type SetUserBlockedRequest struct {
	Principal domain.Principal
	UserID    string
	Blocked   bool
}

// SetUserBlocked blocks or unblocks a contributor.
type SetUserBlocked struct {
	Blocker   datasources.UserBlocker
	Unblocker datasources.UserUnblocker
	Getter    datasources.UserGetter
	// PublicIDSetter and PublicIDs give a user without a public ID one before they are returned, as ResolveUser
	// does.
	PublicIDSetter datasources.PublicIDSetter
	PublicIDs      domain.PublicIDSource
	Now            func() time.Time
}

// Execute blocks or unblocks req.UserID and returns the user. Blocking a blocked user keeps the time they were
// first blocked. A principal other than an interactive moderator returns domain.ErrModeratorRequired, before
// anything is read or written. An unknown user returns an error wrapping datasources.ErrNotFound, and a moderator,
// including the caller, returns ErrModeratorNotBlockable. The write locks the moderator's users row first, so a
// moderator demoted since their request was authenticated is refused with domain.ErrModeratorRequired. A user
// without a public ID, created before public IDs and with no request since, is given one before they are returned.
func (c *SetUserBlocked) Execute(ctx context.Context, req SetUserBlockedRequest) (domain.User, error) {
	if _, err := moderatorActor(req.Principal); err != nil {
		return domain.User{}, err
	}

	user, matched, err := c.setBlockedAndGet(ctx, req)
	if err != nil {
		return domain.User{}, err
	}
	if !matched && user.Role == domain.RoleContributor {
		// The user did not exist when they were blocked or unblocked, but their first request created them
		// before they were read, so the update now matches.
		user, matched, err = c.setBlockedAndGet(ctx, req)
		if err != nil {
			return domain.User{}, err
		}
	}

	switch {
	case matched:
		return c.withPublicID(ctx, user)
	case user.Role == domain.RoleModerator:
		return domain.User{}, ErrModeratorNotBlockable
	default:
		return domain.User{}, errContributorNotMatched
	}
}

// setBlockedAndGet blocks or unblocks req.UserID and then gets the user, reporting whether a contributor with
// that ID matched the update. Only contributors match, so a user who exists but did not match was a moderator
// or did not exist yet when the update ran.
func (c *SetUserBlocked) setBlockedAndGet(ctx context.Context, req SetUserBlockedRequest) (domain.User, bool, error) {
	matched, err := c.setBlocked(ctx, req)
	if err != nil {
		return domain.User{}, false, err
	}

	user, err := c.Getter.GetUser(ctx, req.UserID)
	if err != nil {
		return domain.User{}, false, fmt.Errorf("getting user: %w", err)
	}

	return user, matched, nil
}

// setBlocked blocks or unblocks req.UserID, reporting whether a contributor with that ID matched.
func (c *SetUserBlocked) setBlocked(ctx context.Context, req SetUserBlockedRequest) (bool, error) {
	var err error
	if req.Blocked {
		err = c.Blocker.BlockUser(ctx, req.Principal.User.ID, req.UserID, c.Now())
	} else {
		err = c.Unblocker.UnblockUser(ctx, req.Principal.User.ID, req.UserID)
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

// withPublicID returns user, first giving them a public ID if they have none (withFreshPublicID) and reading them
// again: the spec's User always has one.
func (c *SetUserBlocked) withPublicID(ctx context.Context, user domain.User) (domain.User, error) {
	if user.PublicID != "" {
		return user, nil
	}
	err := withFreshPublicID(ctx, user.ID, c.PublicIDs, func(publicID string) error {
		return c.PublicIDSetter.SetUserPublicIDIfMissing(ctx, user.ID, publicID)
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("setting a public ID: %w", err)
	}
	user, err = c.Getter.GetUser(ctx, user.ID)
	if err != nil {
		return domain.User{}, fmt.Errorf("getting user: %w", err)
	}

	return user, nil
}
