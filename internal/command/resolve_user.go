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
// name and a fresh public ID on their first request. Existing users are returned as stored, so their role, display
// name and blocked state are never changed; one without a public ID, created by code that does not set one since
// the startup backfill ran, is given one.
type ResolveUser struct {
	Getter    datasources.UserGetter
	Ensurer   datasources.UserEnsurer
	PublicIDs domain.PublicIDSource
	Now       func() time.Time
}

// Execute resolves the user with ID userID. In the steady state this is a single read; the user is only
// written when the read finds none, or finds them without a public ID.
func (c *ResolveUser) Execute(ctx context.Context, userID string) (domain.User, error) {
	user, err := c.Getter.GetUser(ctx, userID)
	switch {
	case err == nil && user.PublicID != "":
		return user, nil
	case err != nil && !errors.Is(err, datasources.ErrNotFound):
		return domain.User{}, fmt.Errorf("getting user: %w", err)
	}

	err = withFreshPublicID(ctx, userID, c.PublicIDs, func(publicID string) error {
		user, err = c.Ensurer.EnsureUser(ctx, datasources.NewUser{
			ID:          userID,
			PublicID:    publicID,
			DisplayName: domain.DefaultDisplayName(userID),
			CreatedAt:   c.Now(),
		})
		return err
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("creating user: %w", err)
	}

	return user, nil
}
