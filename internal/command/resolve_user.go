package command

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[string, domain.User] = (*ResolveUser)(nil)

// ResolveUser returns the user with an authenticated ID, creating them as a contributor with the default display
// name on their first request. Existing users are returned as stored, so their role, display name and blocked
// state are never changed.
type ResolveUser struct {
	Getter  datasources.UserGetter
	Ensurer datasources.UserEnsurer
	Now     func() time.Time
}

// Execute resolves the user with ID userID. In the steady state this is a single read; the user is only
// created when the read finds none.
func (c *ResolveUser) Execute(ctx context.Context, userID string) (domain.User, error) {
	user, err := c.Getter.GetUser(ctx, userID)
	if err == nil {
		return user, nil
	}
	if !errors.Is(err, datasources.ErrNotFound) {
		return domain.User{}, fmt.Errorf("getting user: %w", err)
	}

	user, err = c.Ensurer.EnsureUser(ctx, datasources.NewUser{
		ID:          userID,
		DisplayName: domain.DefaultDisplayName(userID),
		CreatedAt:   c.Now(),
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("creating user: %w", err)
	}

	return user, nil
}
