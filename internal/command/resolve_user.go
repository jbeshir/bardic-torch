package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[ResolveUserRequest, domain.User] = (*ResolveUser)(nil)

// ResolveUserRequest is who a request authenticated as.
type ResolveUserRequest struct {
	UserID string
	// DisplayName is the name the identity provider gave for the user, already normalised.
	DisplayName string
}

// ResolveUser returns the user with an authenticated ID, creating them as a contributor on their first request.
// The user's display name is the identity provider's: a new user is created with it, and an existing user whose
// stored name differs has it replaced. Their role and blocked state are never changed.
type ResolveUser struct {
	Users datasources.Users
	Now   func() time.Time
}

// Execute resolves the user req names. In the steady state this is a single read; the user is only created when
// the read finds none, and only written when their name has changed.
func (c *ResolveUser) Execute(ctx context.Context, req ResolveUserRequest) (domain.User, error) {
	user, err := c.Users.GetUser(ctx, req.UserID)
	switch {
	case errors.Is(err, datasources.ErrNotFound):
		user, err = c.Users.EnsureUser(ctx, datasources.NewUser{
			ID:          req.UserID,
			DisplayName: req.DisplayName,
			CreatedAt:   c.Now(),
		})
		if err != nil {
			return domain.User{}, fmt.Errorf("creating user: %w", err)
		}
	case err != nil:
		return domain.User{}, fmt.Errorf("getting user: %w", err)
	}

	if user.DisplayName != req.DisplayName {
		if err := c.Users.UpdateDisplayName(ctx, user.ID, req.DisplayName); err != nil {
			return domain.User{}, fmt.Errorf("updating display name: %w", err)
		}
		user.DisplayName = req.DisplayName
	}

	return user, nil
}
