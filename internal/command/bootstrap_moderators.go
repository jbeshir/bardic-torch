package command

import (
	"context"
	"strings"
	"time"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[[]string, Empty] = (*BootstrapModerators)(nil)

// BootstrapModerators makes the users with the given IDs moderators, so that the first moderators can be
// named in configuration. It only ever promotes: users missing from the list keep their role.
type BootstrapModerators struct {
	Promoter datasources.ModeratorPromoter
	Now      func() time.Time
}

// Execute promotes each user in userIDs, ignoring surrounding whitespace, empty IDs and repeats. Users who have
// not signed in yet are created with the default display name, so their first request finds them as
// moderators, and blocked users are unblocked, because a blocked moderator could never be unblocked through the
// API. Running it again with the same IDs changes nothing. It stops at the first user it cannot promote.
func (c *BootstrapModerators) Execute(ctx context.Context, userIDs []string) (Empty, error) {
	logger := domain.LoggerFromContext(ctx)

	seen := make(map[string]bool, len(userIDs))
	for _, raw := range userIDs {
		userID := strings.TrimSpace(raw)
		if userID == "" || seen[userID] {
			continue
		}
		seen[userID] = true

		if err := c.Promoter.PromoteToModerator(ctx, datasources.NewUser{
			ID:          userID,
			DisplayName: domain.DefaultDisplayName(userID),
			CreatedAt:   c.Now(),
		}); err != nil {
			return Empty{}, err
		}
		logger.InfoContext(ctx, "bootstrapped moderator", "user_id", userID)
	}

	return Empty{}, nil
}
