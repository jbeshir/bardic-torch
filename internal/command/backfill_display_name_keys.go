package command

import (
	"context"
	"errors"
	"fmt"
	"unicode/utf8"

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
// logged by user ID only. A name whose key is too long for its column (domain.MaxDisplayNameKeyRunes), which only
// a name stored before such names were refused can have, is left without a key and logged the same way, so that
// no stored name keeps the service from starting.
func (c *BackfillDisplayNameKeys) Execute(ctx context.Context, _ Empty) (Empty, error) {
	users, err := c.Lister.ListDisplayNameKeys(ctx)
	if err != nil {
		return Empty{}, fmt.Errorf("listing display name keys: %w", err)
	}

	logger := domain.LoggerFromContext(ctx)
	var missing []userKey
	for _, u := range users {
		want := domain.DisplayNameKey(u.DisplayName)
		if utf8.RuneCountInString(want) > domain.MaxDisplayNameKeyRunes {
			logger.WarnContext(ctx, "display name key too long for its column; left unset", "user_id", u.UserID)
			want = ""
		}
		if u.Key != nil && *u.Key == want {
			continue
		}
		if u.Key != nil {
			if err := c.Setter.SetDisplayNameKey(ctx, u.UserID, nil); err != nil {
				return Empty{}, fmt.Errorf("clearing a stale display name key: %w", err)
			}
		}
		if want != "" {
			missing = append(missing, userKey{userID: u.UserID, key: want})
		}
	}

	for _, u := range missing {
		err := c.Setter.SetDisplayNameKey(ctx, u.userID, &u.key)
		switch {
		case errors.Is(err, datasources.ErrDisplayNameTaken):
			logger.WarnContext(ctx, "display name key taken by an earlier user; left unset", "user_id", u.userID)
		case err != nil:
			return Empty{}, fmt.Errorf("setting a display name key: %w", err)
		}
	}

	return Empty{}, nil
}

// userKey is the display name key a user should have.
type userKey struct {
	userID string
	key    string
}
