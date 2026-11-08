package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[Empty, Empty] = (*BackfillPublicIDs)(nil)

// maxPublicIDAttempts is how many fresh public IDs a user is offered before giving them one fails: a public ID is
// 80 random bits, so even one collision is all but impossible.
const maxPublicIDAttempts = 3

// BackfillPublicIDs gives every user without a public ID one: users created before public IDs existed, and users
// created by code that does not set them (a rollback), have none. It runs at startup and is idempotent, so the
// authentication path never needs to write for them.
type BackfillPublicIDs struct {
	Lister    datasources.UsersWithoutPublicIDLister
	Setter    datasources.PublicIDSetter
	PublicIDs domain.PublicIDSource
}

// Execute gives each user without a public ID a fresh one, oldest user first. A public ID another user has is
// logged by user ID only and replaced by another, up to maxPublicIDAttempts times.
func (c *BackfillPublicIDs) Execute(ctx context.Context, _ Empty) (Empty, error) {
	userIDs, err := c.Lister.ListUsersWithoutPublicID(ctx)
	if err != nil {
		return Empty{}, fmt.Errorf("listing users without a public ID: %w", err)
	}

	for _, userID := range userIDs {
		err := withFreshPublicID(ctx, userID, c.PublicIDs, func(publicID string) error {
			return c.Setter.SetUserPublicIDIfMissing(ctx, userID, publicID)
		})
		if err != nil {
			return Empty{}, fmt.Errorf("setting a public ID: %w", err)
		}
	}

	return Empty{}, nil
}

// withFreshPublicID calls set with a fresh public ID from source until it does not fail with
// datasources.ErrPublicIDTaken, at most maxPublicIDAttempts times, logging each collision by userID only.
func withFreshPublicID(ctx context.Context, userID string, source domain.PublicIDSource, set func(string) error) error {
	var err error
	for range maxPublicIDAttempts {
		err = set(source())
		if !errors.Is(err, datasources.ErrPublicIDTaken) {
			return err
		}
		domain.LoggerFromContext(ctx).WarnContext(ctx, "public ID taken by another user; trying another",
			"user_id", userID)
	}

	return err
}
