package command

import (
	"context"
	"fmt"

	"github.com/jbeshir/bardic-torch/internal/datasources"
	"github.com/jbeshir/bardic-torch/internal/domain"
)

var _ Command[Empty, Empty] = (*BackfillPerformanceOwners)(nil)

// BackfillPerformanceOwners gives the performances that have no owner their owner. A release that predates the
// owner columns stores performances without one, even after the columns exist (a rollback, or an import by that
// release), and an approval replaces only the performances its entity owns. It runs at startup and is idempotent.
type BackfillPerformanceOwners struct {
	Backfiller datasources.PerformanceOwnerBackfiller
}

// Execute gives every ownerless performance its owner, and logs how many it gave one.
func (c *BackfillPerformanceOwners) Execute(ctx context.Context, _ Empty) (Empty, error) {
	owned, err := c.Backfiller.BackfillPerformanceOwners(ctx)
	if err != nil {
		return Empty{}, fmt.Errorf("backfilling performance owners: %w", err)
	}
	if owned > 0 {
		domain.LoggerFromContext(ctx).InfoContext(ctx, "gave performances their owners", "performances", owned)
	}

	return Empty{}, nil
}
