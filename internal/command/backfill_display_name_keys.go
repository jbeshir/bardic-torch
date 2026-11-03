package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[Empty, Empty] = (*BackfillDisplayNameKeys)(nil)

// BackfillDisplayNameKeys stores every user's display name key (D20): users created before keys existed, and users
// renamed by code that did not set them (a rollback), have none or a stale one. It runs at startup and is
// idempotent.
type BackfillDisplayNameKeys struct {
	Lister datasources.DisplayNameKeyLister
	Setter datasources.DisplayNameKeySetter
}

// Execute first clears every stale key, so that a key a user no longer holds is free, then sets every missing key,
// oldest user first: of users whose names have one key, the first keeps it and the later ones are left without, and
// logged by user ID only.
func (c *BackfillDisplayNameKeys) Execute(ctx context.Context, _ Empty) (Empty, error) {
	users, err := c.Lister.ListDisplayNameKeys(ctx)
	if err != nil {
		return Empty{}, fmt.Errorf("listing display name keys: %w", err)
	}

	var missing []datasources.UserDisplayNameKey
	for _, u := range users {
		want := domain.DisplayNameKey(u.DisplayName)
		switch {
		case u.Key != nil && *u.Key == want:
		case u.Key != nil:
			if err := c.Setter.SetDisplayNameKey(ctx, u.UserID, nil); err != nil {
				return Empty{}, fmt.Errorf("clearing a stale display name key: %w", err)
			}
			missing = append(missing, u)
		case want != "":
			missing = append(missing, u)
		}
	}

	logger := domain.LoggerFromContext(ctx)
	for _, u := range missing {
		want := domain.DisplayNameKey(u.DisplayName)
		if want == "" {
			continue
		}
		err := c.Setter.SetDisplayNameKey(ctx, u.UserID, &want)
		switch {
		case errors.Is(err, datasources.ErrDisplayNameTaken):
			logger.WarnContext(ctx, "display name key taken by an earlier user; left unset", "user_id", u.UserID)
		case err != nil:
			return Empty{}, fmt.Errorf("setting a display name key: %w", err)
		}
	}

	return Empty{}, nil
}
