package mysql

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

// lockUser locks the users row of userID, the first lock of every write that takes it (docs/PLAN.md §5.1
// "Approval"), and returns the role the row holds. A blocked user is refused with datasources.ErrUserBlocked: the
// request was authenticated before the lock, so a block committed since is only seen under it.
func (c *catalogueReader) lockUser(ctx context.Context, userID string) (domain.Role, error) {
	row, err := c.q.LockUser(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("locking user: %w", lookupError("user", err))
	}
	if row.BlockedAt.Valid {
		return "", datasources.ErrUserBlocked
	}
	role, err := domain.ParseRole(string(row.Role))
	if err != nil {
		return "", fmt.Errorf("locking user: %w", err)
	}

	return role, nil
}

// lockModerator locks the users row of the moderator userID before a moderator-only write takes any other lock,
// refusing a user blocked or no longer a moderator since their request was authenticated
// (datasources.ErrUserBlocked, domain.ErrModeratorRequired).
func (c *catalogueReader) lockModerator(ctx context.Context, userID string) error {
	role, err := c.lockUser(ctx, userID)
	if err != nil {
		return err
	}
	if role != domain.RoleModerator {
		return domain.ErrModeratorRequired
	}

	return nil
}

// lockAuthor locks the users row of actor, who writes a submission or revert, refusing a blocked user
// (datasources.ErrUserBlocked). An interactive moderator's write is approved at once, so one whose row no longer
// holds the moderator role is refused with datasources.ErrActorChanged, for the caller to run it again as the role
// the row holds. A contributor's write stays pending whatever the row's role, so it is never refused for it.
func (c *catalogueReader) lockAuthor(ctx context.Context, actor domain.Actor) error {
	role, err := c.lockUser(ctx, actor.UserID)
	if err != nil {
		return err
	}
	if actor.Role == domain.RoleModerator && role != domain.RoleModerator {
		return datasources.ErrActorChanged
	}

	return nil
}
