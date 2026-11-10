package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[UpdateDisplayNameRequest, domain.User] = (*UpdateDisplayName)(nil)

// UpdateDisplayNameRequest is the request for the UpdateDisplayName command.
type UpdateDisplayNameRequest struct {
	UserID string
	// DisplayName is the name as the user sent it; it is normalised with domain.NormalizeDisplayName.
	DisplayName string
}

// UpdateDisplayName sets a user's display name.
type UpdateDisplayName struct {
	Users datasources.Users
}

// Execute normalises req.DisplayName, stores it, and returns the updated user. A name that cannot be normalised
// returns an error wrapping domain.ErrInvalidDisplayName, and nothing is stored.
func (c *UpdateDisplayName) Execute(ctx context.Context, req UpdateDisplayNameRequest) (domain.User, error) {
	name, err := domain.NormalizeDisplayName(req.DisplayName)
	if err != nil {
		return domain.User{}, err
	}

	if err := c.Users.UpdateDisplayName(ctx, req.UserID, name); err != nil {
		return domain.User{}, fmt.Errorf("updating display name: %w", err)
	}

	user, err := c.Users.GetUser(ctx, req.UserID)
	if err != nil {
		return domain.User{}, fmt.Errorf("getting updated user: %w", err)
	}

	return user, nil
}
